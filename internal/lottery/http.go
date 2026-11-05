package lottery

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

func sendJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		json.NewEncoder(w).Encode(body)
	}
}
func sendError(w http.ResponseWriter, err error) {
	status := 500
	message := "服务内部错误"
	var app *AppError
	if errors.As(err, &app) {
		status = app.Status
		message = app.Message
	} else {
		slog.Error("HTTP 请求失败", "error", err)
	}
	sendJSON(w, status, map[string]string{"error": message})
}
func integerParameter(raw string, min, max int, name string) (int, error) {
	text := domainTrim(raw)
	n, err := strconv.ParseFloat(text, 64)
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") || strings.HasPrefix(text, "0b") || strings.HasPrefix(text, "0B") || strings.HasPrefix(text, "0o") || strings.HasPrefix(text, "0O") {
		integer, e := strconv.ParseUint(text, 0, 64)
		n, err = float64(integer), e
	}
	if err != nil || strings.Contains(text, "_") || math.IsNaN(n) || n != math.Trunc(n) || n < float64(min) || n > float64(max) {
		return 0, appError(400, name+"超出有效范围")
	}
	return int(n), nil
}

var betIDPath = regexp.MustCompile(`^[1-9][0-9]*$`)

func NewHTTPHandler(service *Service, assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(err error) { sendError(w, err) }
		methodError := func(allowed string) {
			w.Header().Set("Allow", allowed)
			fail(appError(405, "该接口仅支持 "+allowed))
		}
		route := r.URL.Path
		if route == "/api/health" {
			if r.Method != "GET" {
				methodError("GET")
				return
			}
			sendJSON(w, 200, map[string]bool{"ok": true})
			return
		}
		if route == "/api/bets" {
			switch r.Method {
			case "GET":
				q := r.URL.Query()
				for key, values := range q {
					if len(values) > 1 {
						q.Set(key, values[len(values)-1])
					}
				}
				o := ListOptions{Game: q.Get("game"), Status: q.Get("status")}
				var err error
				if q.Has("game") && o.Game != "dlt" && o.Game != "ssq" {
					fail(appError(400, "彩种错误"))
					return
				}
				validStatuses := map[string]bool{"pending": true, "partial": true, "prize_pending": true, "won": true, "lost": true}
				if q.Has("status") && !validStatuses[o.Status] {
					fail(appError(400, "状态错误"))
					return
				}
				if !q.Has("page") {
					q.Set("page", "1")
				}
				if !q.Has("pageSize") {
					q.Set("pageSize", "10")
				}
				if o.Page, err = integerParameter(q.Get("page"), 1, 1000000, "页码"); err != nil {
					fail(err)
					return
				}
				if o.PageSize, err = integerParameter(q.Get("pageSize"), 1, 50, "每页数量"); err != nil {
					fail(err)
					return
				}
				page, err := service.Repository.List(o)
				if err != nil {
					fail(err)
					return
				}
				sendJSON(w, 200, page)
			case "POST":
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || media != "application/json" {
					fail(appError(415, "请使用 application/json"))
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					var size *http.MaxBytesError
					if errors.As(err, &size) {
						fail(appError(413, "请求体不能超过 512 KiB"))
					} else {
						fail(appError(400, "无法读取请求体"))
					}
					return
				}
				// Unmarshal once to reject null, arrays and trailing JSON with a clear error.
				var object map[string]json.RawMessage
				if err = json.Unmarshal(raw, &object); err != nil || object == nil {
					fail(appError(400, "请求体必须为 JSON 对象"))
					return
				}
				b, err := service.Create(raw)
				if err != nil {
					fail(err)
					return
				}
				sendJSON(w, 201, b)
			default:
				methodError("GET, POST")
			}
			return
		}
		if strings.HasPrefix(route, "/api/bets/") {
			raw := strings.TrimPrefix(route, "/api/bets/")
			if !betIDPath.MatchString(raw) {
				fail(appError(404, "接口不存在"))
				return
			}
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id < 1 || id > MaxSafeInteger {
				fail(appError(400, "记录 ID 无效"))
				return
			}
			switch r.Method {
			case "GET":
				b, e := service.Repository.Get(id)
				if e != nil {
					fail(e)
					return
				}
				sendJSON(w, 200, b)
			case "DELETE":
				if e := service.Repository.Remove(id); e != nil {
					fail(e)
					return
				}
				sendJSON(w, 204, nil)
			default:
				methodError("GET, DELETE")
			}
			return
		}
		if strings.HasPrefix(route, "/api/") {
			fail(appError(404, "接口不存在"))
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			methodError("GET, HEAD")
			return
		}
		name := route
		if route == "/lottery" || strings.HasPrefix(route, "/lottery/") {
			name = strings.TrimPrefix(route, "/lottery")
		}
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			name = "index.html"
		}
		switch name {
		case "index.html", "favicon.svg", "styles.css", "lottery-core.js", "app.js", "records.js":
		default:
			fail(appError(404, "文件不存在"))
			return
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			fail(err)
			return
		}
		typ := mime.TypeByExtension(path.Ext(name))
		if typ == "" {
			typ = "application/octet-stream"
		}
		w.Header().Set("Content-Type", typ)
		w.WriteHeader(200)
		if r.Method != "HEAD" {
			w.Write(data)
		}
	})
}
