package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"lingxi/server/internal/reqctx"
)

// handleClientLog is the browser log sink. It is public and therefore
// hostile-input: body is capped by middleware, entries are capped here, and
// text is truncated and stripped of control characters (log injection).
func handleClientLog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Entries []struct {
			Level string         `json:"level"`
			Msg   string         `json:"msg"`
			Ctx   map[string]any `json:"ctx"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var uid uint64
	if u, ok := reqctx.UserFrom(r.Context()); ok {
		uid = u.ID
	}
	for i, e := range body.Entries {
		if i >= 50 {
			break
		}
		lvl := slog.LevelInfo
		switch e.Level {
		case "warn":
			lvl = slog.LevelWarn
		case "error":
			lvl = slog.LevelError
		case "debug":
			lvl = slog.LevelDebug
		}
		ctxJSON, _ := json.Marshal(e.Ctx)
		slog.Log(r.Context(), lvl, "client", "userId", uid, "msg", clean(e.Msg, 500), "ctx", clean(string(ctxJSON), 2000))
	}
	w.WriteHeader(http.StatusNoContent)
}

func clean(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
