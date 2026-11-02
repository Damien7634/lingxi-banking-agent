// Package audit writes the append-only audit trail. Writing is best-effort for
// the caller (a failed audit insert is logged loudly but does not change the
// business response), and parameters are scrubbed before they are stored.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"

	"lingxi/server/internal/reqctx"
)

type Logger struct{ db *sql.DB }

func New(db *sql.DB) *Logger { return &Logger{db: db} }

type Entry struct {
	UserID uint64 // 0 = anonymous
	Action string
	Target string
	Params json.RawMessage
	Err    error
}

var sensitiveKeys = []string{"password", "pass", "pwd", "token", "secret", "pin", "cvv", "otp", "cookie", "authorization"}

func (l *Logger) Write(ctx context.Context, e Entry) {
	meta := reqctx.MetaFrom(ctx)
	result, errMsg := "ok", ""
	if e.Err != nil {
		result, errMsg = "fail", truncate(e.Err.Error(), 255)
	}
	var params any
	if len(e.Params) > 0 {
		if s := Scrub(e.Params); s != nil {
			params = string(s)
		}
	}
	var uid any
	if e.UserID != 0 {
		uid = e.UserID
	}
	// Use a detached context: an audit row must survive client disconnects.
	_, err := l.db.ExecContext(context.WithoutCancel(ctx),
		`INSERT INTO audit_logs (user_id, action, target, params, result, error, ip, user_agent, request_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, truncate(e.Action, 64), truncate(e.Target, 128), params, result, errMsg, meta.IP, meta.UserAgent, meta.RequestID)
	if err != nil {
		slog.ErrorContext(ctx, "AUDIT WRITE FAILED", "action", e.Action, "userId", e.UserID, "error", err)
	}
}

// Scrub replaces values of sensitive keys at any depth. Invalid JSON → nil.
func Scrub(raw json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	out, err := json.Marshal(scrubValue(v))
	if err != nil {
		return nil
	}
	if len(out) > 4096 {
		out, _ = json.Marshal(map[string]string{"truncated": string(out[:4000])})
	}
	return out
}

func scrubValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			lk := strings.ToLower(k)
			hit := false
			for _, s := range sensitiveKeys {
				if strings.Contains(lk, s) {
					hit = true
					break
				}
			}
			if hit {
				t[k] = "[REDACTED]"
			} else {
				t[k] = scrubValue(val)
			}
		}
		return t
	case []any:
		for i := range t {
			t[i] = scrubValue(t[i])
		}
		return t
	default:
		return v
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// keep valid UTF-8
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}
