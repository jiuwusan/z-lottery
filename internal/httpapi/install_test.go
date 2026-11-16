package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jiuwusan/z-lottery/internal/httpapi"
	assets "github.com/jiuwusan/z-lottery/lottery"
)

func TestInstallAssets(t *testing.T) {
	for _, prefix := range []string{"", "/tools/draw"} {
		t.Run(prefix, func(t *testing.T) {
			handler := http.StripPrefix(prefix, httpapi.NewHTTPHandler(nil, assets.Files))
			for name, contentType := range map[string]string{
				"manifest.webmanifest": "application/manifest+json",
				"install.js":           "javascript",
				"icon-180.png":         "image/png",
				"icon-192.png":         "image/png",
				"icon-512.png":         "image/png",
			} {
				for _, method := range []string{"GET", "HEAD"} {
					r := httptest.NewRecorder()
					handler.ServeHTTP(r, httptest.NewRequest(method, prefix+"/"+name, nil))
					if r.Code != 200 || !strings.Contains(r.Header().Get("Content-Type"), contentType) {
						t.Errorf("%s %s: %d %s", method, name, r.Code, r.Header().Get("Content-Type"))
					}
					if (method == "HEAD") != (r.Body.Len() == 0) {
						t.Errorf("%s %s: unexpected body length %d", method, name, r.Body.Len())
					}
				}
			}
			for _, name := range []string{"install_test.go", "embed.go", "README.md", "missing.png"} {
				r := httptest.NewRecorder()
				handler.ServeHTTP(r, httptest.NewRequest("GET", prefix+"/"+name, nil))
				if r.Code != 404 {
					t.Errorf("unexpected public file %s: %d", name, r.Code)
				}
			}
		})
	}
}

func TestInstallManifest(t *testing.T) {
	raw, err := assets.Files.ReadFile("manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name     string `json:"name"`
		ID       string `json:"id"`
		StartURL string `json:"start_url"`
		Scope    string `json:"scope"`
		Display  string `json:"display"`
		Icons    []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name == "" || manifest.Display != "standalone" || manifest.StartURL != "./" || manifest.Scope != "./" {
		t.Fatalf("invalid standalone manifest: %s", raw)
	}
	for _, base := range []string{"https://example.test/", "https://example.test/tools/draw/"} {
		baseURL, _ := url.Parse(base + "manifest.webmanifest")
		for _, value := range []string{manifest.StartURL, manifest.Scope} {
			relative, err := url.Parse(value)
			if err != nil || baseURL.ResolveReference(relative).String() != base {
				t.Fatalf("manifest URL %q leaves mount %s", value, base)
			}
		}
		start, _ := url.Parse(manifest.StartURL)
		identity := baseURL.ResolveReference(start)
		if manifest.ID != "" {
			// Manifest IDs resolve against start_url's origin, not the manifest directory.
			// Omitting ID defaults to start_url, keeping separate proxy mounts distinct.
			origin := &url.URL{Scheme: identity.Scheme, Host: identity.Host, Path: "/"}
			id, err := url.Parse(manifest.ID)
			if err != nil {
				t.Fatal(err)
			}
			identity = origin.ResolveReference(id)
		}
		if identity.String() != base {
			t.Fatalf("app identity %s must match mount %s", identity, base)
		}
	}
	for size, name := range map[int]string{180: "icon-180.png", 192: "icon-192.png", 512: "icon-512.png"} {
		raw, err := assets.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		config, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || config.Width != size || config.Height != size {
			t.Fatalf("%s: invalid PNG size: %+v, %v", name, config, err)
		}
		if size == 180 {
			continue
		}
		found := false
		for _, icon := range manifest.Icons {
			if strings.TrimPrefix(icon.Src, "./") == name && icon.Type == "image/png" && icon.Sizes == fmt.Sprintf("%dx%d", size, size) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing install icon %s", name)
		}
	}
}
