package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Server-enforced confirmation for high-risk tools.
//
// The first call of a confirm-required tool never executes: it returns a
// single-use token bound to (user, tool, canonical params, user-turn number).
// The token can only be redeemed in a LATER user turn than the one it was
// issued in, so the model cannot act on its own (hallucination, prompt
// injection in tool output): a real user message must arrive in between.
// Because the browser only replays plain text history, pending confirmations
// are re-injected into the next turn's system prompt by pendingConfirmations.

const confirmTTL = 5 * time.Minute

var (
	errTokenInvalid  = errors.New("确认令牌无效或已过期，请重新发起操作")
	errTokenSameTurn = errors.New("必须先向用户确认，并在用户明确同意后的下一轮对话中再执行")
	errTokenMismatch = errors.New("确认的参数与本次调用不一致，请按用户确认的内容重新发起")
)

// canonicalParams strips confirmToken and re-marshals (Go sorts map keys), so
// the hash only depends on the business parameters.
func canonicalParams(raw json.RawMessage) (hash string, canonical []byte, err error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	delete(m, "confirmToken")
	b, err := json.Marshal(m)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, nil
}

func issueToken(ctx context.Context, d *sql.DB, uid uint64, tool, paramsHash string, params []byte, summary string, turn int) (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := "cf_" + base64.RawURLEncoding.EncodeToString(b)
	now := time.Now().UTC()
	_, err := d.ExecContext(ctx, `INSERT INTO agent_confirmations (token, user_id, tool, params_hash, params_json, summary, turn_no, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, tok, uid, tool, paramsHash, string(params), clip(summary, 500), turn, now, now.Add(confirmTTL))
	return tok, err
}

// redeemToken atomically consumes the token if every binding matches.
func redeemToken(ctx context.Context, d *sql.DB, uid uint64, tool, paramsHash, tok string, turn int) error {
	if len(tok) > 64 {
		return errTokenInvalid
	}
	var gotTool, gotParams string
	var issuedTurn int
	var expires time.Time
	var used sql.NullTime
	err := d.QueryRowContext(ctx, `SELECT tool, params_hash, turn_no, expires_at, used_at FROM agent_confirmations WHERE token = ? AND user_id = ?`,
		tok, uid).Scan(&gotTool, &gotParams, &issuedTurn, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return errTokenInvalid
	}
	if err != nil {
		return err
	}
	if used.Valid || time.Now().UTC().After(expires) || gotTool != tool {
		return errTokenInvalid
	}
	if turn <= issuedTurn {
		return errTokenSameTurn
	}
	if gotParams != paramsHash {
		return errTokenMismatch
	}
	res, err := d.ExecContext(ctx, `UPDATE agent_confirmations SET used_at = ? WHERE token = ? AND user_id = ? AND used_at IS NULL`, time.Now().UTC(), tok, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errTokenInvalid // lost a race with a concurrent redeem
	}
	return nil
}

type pending struct {
	Token   string
	Tool    string
	Params  string
	Summary string
}

// pendingConfirmations lists this user's unexpired, unused tokens issued in
// an earlier turn — what the user may be answering "确认" to right now.
func pendingConfirmations(ctx context.Context, d *sql.DB, uid uint64, turn int) ([]pending, error) {
	rows, err := d.QueryContext(ctx, `SELECT token, tool, params_json, summary FROM agent_confirmations
		WHERE user_id = ? AND used_at IS NULL AND expires_at > ? AND turn_no < ? ORDER BY created_at DESC LIMIT 3`,
		uid, time.Now().UTC(), turn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.Token, &p.Tool, &p.Params, &p.Summary); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
