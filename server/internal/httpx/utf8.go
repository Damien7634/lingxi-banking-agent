package httpx

import (
	"bytes"
	"io"
	"net/http"
	"unicode/utf8"
)

// RequireUTF8 rejects request bodies that are not valid UTF-8, so mis-encoded
// text never reaches the ledger or the audit trail. Use after BodyLimit.
func RequireUTF8(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "请求体过大"})
			return
		}
		if !utf8.Valid(body) {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "请求包含非法字符编码，请使用 UTF-8"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}
