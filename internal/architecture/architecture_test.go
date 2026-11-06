// Package architecture turns the documented dependency direction into a CI gate.
package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var allowed = map[string]map[string]bool{"domain": {},
	"gateway":      {"domain": true},
	"repository":   {"domain": true},
	"lottery":      {"domain": true},
	"httpapi":      {"domain": true},
	"architecture": {}}

const localPrefix = "github.com/jiuwusan/z-lottery/internal/"

func violation(layer, dep string) bool {
	if strings.HasPrefix(dep, localPrefix) {
		target := strings.Split(strings.TrimPrefix(dep, localPrefix), "/")[0]
		return target != layer && !allowed[layer][target]
	}
	if layer == "domain" || layer == "config" || layer == "service" || layer == "lottery" {
		switch dep {
		case "os", "os/exec", "net/http", "net", "database/sql", "io/fs":
			return true
		}
	}
	return false
}
func TestDependencies(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Join(root, "internal"), path)
		layer := strings.Split(filepath.ToSlash(rel), "/")[0]
		if _, ok := allowed[layer]; !ok {
			t.Errorf("unclassified package: %s", rel)
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dep, _ := strconv.Unquote(spec.Path.Value)
			if violation(layer, dep) {
				t.Errorf("%s: forbidden dependency %s -> %s", rel, layer, dep)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestDependencyRules(t *testing.T) {
	if !violation("domain", localPrefix+"repository") {
		t.Fatal("domain must reject persistence")
	}
	if !violation("domain", "net/http") {
		t.Fatal("domain must reject HTTP I/O")
	}
	if violation("domain", "time") {
		t.Fatal("domain must allow value/time operations")
	}
	if !violation("domain", localPrefix+"unclassified") {
		t.Fatal("unknown internal layers must fail")
	}
}
