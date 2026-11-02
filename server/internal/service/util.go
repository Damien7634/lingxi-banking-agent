package service

import (
	"encoding/json"
	"strconv"

	"lingxi/server/internal/audit"
)

func uintStr(v uint64) string { return strconv.FormatUint(v, 10) }

func clipStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

func auditEntry(uid uint64, action, target string, err error) audit.Entry {
	return audit.Entry{UserID: uid, Action: action, Target: target, Err: err}
}
