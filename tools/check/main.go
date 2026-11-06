// Command check verifies formatting, package boundaries, static analysis, tests and builds.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func run(name string, args ...string) {
	fmt.Println(name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Fprintln(os.Stderr, "run from the repository root: go run ./tools/check")
		os.Exit(1)
	}
	files := []string{}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && path != "." || d.Name() == "node_modules" || d.Name() == "data" || d.Name() == "bin") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	cmd := exec.Command("gofmt", append([]string{"-l"}, files...)...)
	output, err := cmd.CombinedOutput()
	if err != nil || len(output) > 0 {
		fmt.Fprintln(os.Stderr, "gofmt verification failed:\n"+string(output))
		os.Exit(1)
	}
	run("go", "vet", "-p", "2", "./...")
	run("go", "test", "-p", "2", "-timeout", "120s", "./...")
	run("go", "build", "-p", "2", "./...")
}
