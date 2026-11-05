package lottery

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHTTPCompatibleCRUDAndNoExternalJob(t *testing.T) {
	r := testRepo(t)
	s := NewService(r, fakeProvider{})
	assets := fstest.MapFS{"index.html": {Data: []byte("<html>lottery</html>")}, "app.js": {Data: []byte("app")}}
	server := httptest.NewServer(NewHTTPHandler(s, fs.FS(assets)))
	defer server.Close()
	request := func(method, path, raw string, code int) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(raw))
		if raw != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != code {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, body)
		}
		return body
	}
	raw := request("POST", "/api/bets", issueInput, 201)
	var b Bet
	if e := json.Unmarshal(raw, &b); e != nil || b.TotalAmount != 2 {
		t.Fatal(string(raw), e)
	}
	request("GET", "/api/bets?page=1&pageSize=1", "", 200)
	for _, query := range []string{"page=1.0", "page=1e0", "page=%201%20", "page=0x1"} {
		request("GET", "/api/bets?"+query, "", 200)
	}
	for _, query := range []string{"page=", "pageSize=", "game=", "status="} {
		request("GET", "/api/bets?"+query, "", 400)
	}
	for _, query := range []string{"page=1_0", "status=pending%7Cpartial"} {
		request("GET", "/api/bets?"+query, "", 400)
	}
	for _, route := range []string{"/api/bets/0", "/api/bets/01", "/api/bets/+1", "/api/bets/nope"} {
		request("GET", route, "", 404)
	}
	request("GET", "/api/bets/1", "", 200)
	request("GET", "/api/health", "", 200)
	request("HEAD", "/", "", 200)
	request("GET", "/lottery/app.js", "", 200)
	for _, method := range []string{"GET", "POST"} {
		request(method, "/api/jobs/check", "{}", 404)
	}
	for _, path := range []string{"/data/lottery.sqlite", "/server/index.js", "/go.mod"} {
		request("GET", path, "", 404)
	}
	for _, path := range []string{"/api/bets?page=0", "/api/bets?pageSize=51", "/api/bets?game=bad", "/api/bets?status=bad", "/api/bets/9007199254740992"} {
		request("GET", path, "", 400)
	}
	for _, raw := range []string{"{bad", "null", "[]", "{} {}", `{"game":"dlt","tickets":[]}`} {
		request("POST", "/api/bets", raw, 400)
	}
	request("POST", "/api/bets", string(bytes.Repeat([]byte("x"), 512*1024+1)), 413)
	request("PUT", "/api/bets", "", 405)
	request("DELETE", "/api/bets/1", "", 204)
	request("GET", "/api/bets/1", "", 404)
}
