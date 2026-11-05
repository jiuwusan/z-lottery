package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jiuwusan/z-lottery/internal/lottery"
	assets "github.com/jiuwusan/z-lottery/lottery"
	"github.com/joho/godotenv"
)

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

type config struct {
	host, port, database, expression, zone string
	enabled                                bool
}

func loadConfig() (config, error) {
	c := config{host: env("HOST", "127.0.0.1"), port: env("PORT", "3000"), database: env("DATABASE_PATH", "./data/lottery.sqlite"), expression: env("CHECK_SCHEDULE", lottery.DefaultCheckSchedule), zone: env("CHECK_TIMEZONE", "Asia/Shanghai")}
	n, e := strconv.Atoi(c.port)
	if e != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("PORT 必须在 1～65535 之间")
	}
	if c.database == "" {
		return c, fmt.Errorf("DATABASE_PATH 不能为空")
	}
	c.enabled, e = strconv.ParseBool(env("CHECK_ENABLED", "true"))
	if e != nil {
		return c, fmt.Errorf("CHECK_ENABLED 必须是 true 或 false")
	}
	return c, nil
}
func run() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取 .env: %w", err)
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		if os.Args[1] != "healthcheck" {
			return fmt.Errorf("未知命令: %s", os.Args[1])
		}
		host := c.host
		if host == "0.0.0.0" || host == "::" || host == "" {
			host = "127.0.0.1"
		}
		client := http.Client{Timeout: 4 * time.Second}
		res, e := client.Get("http://" + net.JoinHostPort(host, c.port) + "/api/health")
		if e != nil {
			return e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("health HTTP %d", res.StatusCode)
		}
		return nil
	}
	schedule, err := lottery.ParseSchedule(c.expression, c.zone)
	if err != nil {
		return err
	}
	repository, err := lottery.OpenRepository(c.database)
	if err != nil {
		return fmt.Errorf("打开 SQLite: %w", err)
	}
	defer repository.Close()
	service := lottery.NewService(repository, lottery.NewDrawProvider(nil))
	logger := slog.Default()
	server := &http.Server{Addr: net.JoinHostPort(c.host, c.port), Handler: lottery.NewHTTPHandler(service, assets.Files), ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 * 1024}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()
	jobDone := make(chan struct{})
	if c.enabled {
		scheduler := lottery.NewScheduler(schedule, service, logger)
		logger.Info("内置验奖调度已启用", "schedule", c.expression, "timezone", c.zone, "next", schedule.Next(time.Now()))
		go func() { defer close(jobDone); scheduler.Run(jobCtx) }()
	} else {
		logger.Info("内置验奖调度已禁用")
		close(jobDone)
	}
	httpDone := make(chan error, 1)
	go func() { httpDone <- server.Serve(listener) }()
	logger.Info("z-lottery Go 服务已启动", "address", listener.Addr())
	select {
	case <-signalCtx.Done():
	case err = <-httpDone:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancelJobs()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if e := server.Shutdown(shutdownCtx); e != nil {
		server.Close()
		if err == nil {
			err = e
		}
	}
	// Provider requests share jobCtx; join before closing SQLite to avoid writes
	// against a closed connection during SIGTERM.
	<-jobDone
	return err
}
func main() {
	if err := run(); err != nil {
		slog.Error("z-lottery 退出", "error", err)
		os.Exit(1)
	}
}
