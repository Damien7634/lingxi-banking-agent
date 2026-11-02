// Package httpx holds HTTP middleware shared by every route.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"lingxi/server/internal/reqctx"
)

const RequestIDHeader = "X-Request-Id"

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ClientIP returns the peer address; X-Forwarded-For is used only when the
// deployment explicitly trusts its proxy (otherwise it is attacker-controlled).
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// RequestMeta assigns a request id, captures client identity and writes one
// access-log line per /api request.
func RequestMeta(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newID()
			w.Header().Set(RequestIDHeader, id)
			ua := r.UserAgent()
			if len(ua) > 255 {
				ua = ua[:255]
			}
			ctx := reqctx.WithMeta(r.Context(), reqctx.Meta{RequestID: id, IP: ClientIP(r, trustProxy), UserAgent: ua})
			sw := &statusWriter{ResponseWriter: w, status: 200}
			start := time.Now()
			next.ServeHTTP(sw, r.WithContext(ctx))
			if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/log" {
				attrs := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "durationMs", time.Since(start).Milliseconds()}
				if u, ok := reqctx.UserFrom(ctx); ok {
					attrs = append(attrs, "userId", u.ID)
				}
				slog.InfoContext(ctx, "request", attrs...)
			}
		})
	}
}

// Recover turns panics into a generic 500 without leaking details.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.ErrorContext(r.Context(), "panic", "panic", rec, "path", r.URL.Path)
				WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal Server Error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets defensive headers on every response.
func SecurityHeaders(production bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			if strings.HasPrefix(r.URL.Path, "/api/") {
				h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
				h.Set("Cache-Control", "no-store")
			} else {
				h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			}
			if production {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OriginGuard is the CSRF defence for state-changing requests: the browser's
// Origin (or Sec-Fetch-Site) must match the allowlist. Combined with
// SameSite=Strict cookies this blocks cross-site form/fetch submissions.
func OriginGuard(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if !slices.Contains(allowed, strings.TrimRight(origin, "/")) && !sameHost(origin, r.Host) {
					WriteJSON(w, http.StatusForbidden, map[string]string{"error": "跨站请求被拒绝"})
					return
				}
			} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				WriteJSON(w, http.StatusForbidden, map[string]string{"error": "跨站请求被拒绝"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && u.Host == host
}

// IPRateLimit limits all /api requests per client IP.
func IPRateLimit(l *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow("ip:" + reqctx.MetaFrom(r.Context()).IP) {
				w.Header().Set("Retry-After", "10")
				WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "请求过于频繁，请稍后再试"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit caps the request body before any handler reads it.
func BodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "请求体过大"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
