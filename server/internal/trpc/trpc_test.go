package trpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"lingxi/server/internal/apperr"
)

type addIn struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

func (in *addIn) Validate() error {
	if in.A < 0 {
		return apperr.Bad("a must be >= 0")
	}
	return nil
}

func newTestRouter(authed bool) *Router {
	r := NewRouter(Hooks{Authenticated: func(context.Context) bool { return authed }})
	r.PublicQuery("ping", NoInput(func(context.Context) (map[string]bool, error) { return map[string]bool{"ok": true}, nil }))
	r.Query("math.add", Typed(func(_ context.Context, in addIn) (int64, error) { return in.A + in.B, nil }))
	r.Mutation("math.boom", NoInput(func(context.Context) (any, error) { panic("secret internal detail") }))
	return r
}

func do(t *testing.T, r *Router, method, target, body string) (int, []map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return w.Code, out
}

func q(input string) string { return url.QueryEscape(input) }

func TestBatchQuery(t *testing.T) {
	r := newTestRouter(true)
	code, out := do(t, r, http.MethodGet, "/api/trpc/ping,math.add?batch=1&input="+q(`{"1":{"json":{"a":2,"b":3}}}`), "")
	if code != 200 || len(out) != 2 {
		t.Fatalf("code=%d out=%v", code, out)
	}
	if got := out[1]["result"].(map[string]any)["data"].(map[string]any)["json"]; got != float64(5) {
		t.Fatalf("got %v", got)
	}
}

func TestUnauthenticatedRejected(t *testing.T) {
	r := newTestRouter(false)
	code, out := do(t, r, http.MethodGet, "/api/trpc/ping,math.add?batch=1&input="+q(`{"1":{"json":{"a":1,"b":1}}}`), "")
	if code != http.StatusMultiStatus {
		t.Fatalf("want 207 got %d", code)
	}
	errData := out[1]["error"].(map[string]any)["json"].(map[string]any)["data"].(map[string]any)
	if errData["code"] != "UNAUTHORIZED" || errData["httpStatus"] != float64(401) {
		t.Fatalf("got %v", errData)
	}
}

func TestStrictDecoding(t *testing.T) {
	r := newTestRouter(true)
	for _, in := range []string{
		`{"0":{"json":{"a":1,"b":2,"userId":9}}}`, // unknown field (mass assignment)
		`{"0":{"json":{"a":"1","b":2}}}`,          // wrong type
		`{"0":{"json":{"a":1.5,"b":2}}}`,          // float into int
		`{"0":{"json":{"a":-1,"b":2}}}`,           // Validate()
	} {
		code, out := do(t, r, http.MethodGet, "/api/trpc/math.add?batch=1&input="+q(in), "")
		if code != 400 || out[0]["error"] == nil {
			t.Errorf("input %s: code=%d out=%v", in, code, out)
		}
	}
}

func TestMethodMismatch(t *testing.T) {
	r := newTestRouter(true)
	code, _ := do(t, r, http.MethodPost, "/api/trpc/math.add?batch=1", `{"0":{"json":{"a":1,"b":1}}}`)
	if code != 405 {
		t.Fatalf("query via POST should be 405, got %d", code)
	}
}

func TestPanicDoesNotLeak(t *testing.T) {
	r := newTestRouter(true)
	req := httptest.NewRequest(http.MethodPost, "/api/trpc/math.boom?batch=1", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBatchLimit(t *testing.T) {
	r := newTestRouter(true)
	paths := strings.Repeat("ping,", 25) + "ping"
	code, _ := do(t, r, http.MethodGet, "/api/trpc/"+paths+"?batch=1", "")
	if code != 400 {
		t.Fatalf("want 400 got %d", code)
	}
}

func TestRejectsInvalidUTF8(t *testing.T) {
	r := NewRouter(Hooks{Authenticated: func(context.Context) bool { return true }})
	r.Mutation("echo", Typed(func(_ context.Context, in struct {
		Memo string `json:"memo"`
	}) (string, error) {
		return in.Memo, nil
	}))
	body := "{\"0\":{\"json\":{\"memo\":\"\xb1\xe0\xc2\xeb\"}}}" // "编码" in GBK bytes
	code, out := do(t, r, http.MethodPost, "/api/trpc/echo?batch=1", body)
	if code != 400 || out[0]["error"] == nil {
		t.Fatalf("invalid UTF-8 accepted: %d %v", code, out)
	}
}
