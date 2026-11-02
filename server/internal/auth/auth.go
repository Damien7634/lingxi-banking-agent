// Package auth implements password login and server-side sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/audit"
	"lingxi/server/internal/config"
	"lingxi/server/internal/db"
	"lingxi/server/internal/httpx"
	"lingxi/server/internal/reqctx"
)

const (
	maxFailedAttempts = 5
	lockoutDuration   = 15 * time.Minute
	touchInterval     = time.Minute
)

type Service struct {
	db         *sql.DB
	cfg        *config.Config
	audit      *audit.Logger
	ipLimit    *httpx.Limiter
	nameLimit  *httpx.Limiter
	cookieName string
	now        func() time.Time
}

func NewService(d *sql.DB, cfg *config.Config, a *audit.Logger) *Service {
	name := "lx_session"
	if cfg.CookieSecure {
		name = "__Host-lx_session" // __Host- prefix: Secure, Path=/, no Domain — cannot be planted by subdomains
	}
	return &Service{
		db: d, cfg: cfg, audit: a,
		ipLimit:    httpx.NewLimiter(20, 10), // 20 login attempts/min per IP
		nameLimit:  httpx.NewLimiter(10, 5),  // 10/min per username
		cookieName: name,
		now:        time.Now,
	}
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Middleware resolves the session cookie (if any) into a reqctx.User. It never
// rejects; procedures decide whether auth is required.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cookieName)
		if err == nil && c.Value != "" && len(c.Value) <= 128 {
			if u, err := s.resolve(r.Context(), hashToken(c.Value)); err == nil {
				r = r.WithContext(reqctx.WithUser(r.Context(), u))
			} else if !errors.Is(err, errNoSession) {
				slog.ErrorContext(r.Context(), "session lookup failed", "error", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

var errNoSession = errors.New("no session")

func (s *Service) resolve(ctx context.Context, idHash string) (*reqctx.User, error) {
	var (
		u          reqctx.User
		lastSeen   time.Time
		expiresAt  time.Time
		revokedAt  sql.NullTime
		userStatus string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT s.user_id, s.last_seen_at, s.expires_at, s.revoked_at, u.username, u.display_name, u.status
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = ?`, idHash).
		Scan(&u.ID, &lastSeen, &expiresAt, &revokedAt, &u.Username, &u.DisplayName, &userStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSession
	}
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if revokedAt.Valid || userStatus != "active" || !now.Before(expiresAt) || now.Sub(lastSeen) > s.cfg.SessionIdleTTL {
		return nil, errNoSession
	}
	if now.Sub(lastSeen) > touchInterval {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?`, now, idHash)
	}
	u.SessionHash = idHash
	return &u, nil
}

type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserView struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

var errBadCredentials = apperr.New(apperr.Unauthorized, "用户名或密码错误")

func (s *Service) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, in LoginInput) (*UserView, error) {
	meta := reqctx.MetaFrom(ctx)
	username := strings.TrimSpace(in.Username)
	if username == "" || in.Password == "" || utf8.RuneCountInString(username) > 64 || len(in.Password) > 128 {
		return nil, errBadCredentials
	}
	if !s.ipLimit.Allow(meta.IP) || !s.nameLimit.Allow(strings.ToLower(username)) {
		s.audit.Write(ctx, audit.Entry{Action: "auth.login", Target: username, Err: apperr.ErrTooMany})
		return nil, apperr.ErrTooMany
	}

	var (
		id          uint64
		hash        string
		displayName string
		status      string
		failed      int
		lockedUntil sql.NullTime
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash, display_name, status, failed_attempts, locked_until FROM users WHERE username = ?`, username).
		Scan(&id, &hash, &displayName, &status, &failed, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_, _ = VerifyPassword(in.Password, dummyHash)
		s.audit.Write(ctx, audit.Entry{Action: "auth.login", Target: username, Err: errors.New("unknown user")})
		return nil, errBadCredentials
	}
	if err != nil {
		return nil, err
	}

	ok, verr := VerifyPassword(in.Password, hash)
	if verr != nil {
		return nil, fmt.Errorf("verify password: %w", verr)
	}
	now := s.now().UTC()
	if lockedUntil.Valid && now.Before(lockedUntil.Time) {
		mins := int(lockedUntil.Time.Sub(now).Minutes()) + 1
		e := apperr.New(apperr.Forbidden, "登录失败次数过多，账户已临时锁定，请 %d 分钟后再试", mins)
		s.audit.Write(ctx, audit.Entry{UserID: id, Action: "auth.login", Target: username, Err: e})
		return nil, e
	}
	if status != "active" {
		s.audit.Write(ctx, audit.Entry{UserID: id, Action: "auth.login", Target: username, Err: errors.New("disabled")})
		return nil, errBadCredentials
	}
	if !ok {
		failed++
		if failed >= maxFailedAttempts {
			_, err = s.db.ExecContext(ctx, `UPDATE users SET failed_attempts = 0, locked_until = ? WHERE id = ?`, now.Add(lockoutDuration), id)
		} else {
			_, err = s.db.ExecContext(ctx, `UPDATE users SET failed_attempts = ? WHERE id = ?`, failed, id)
		}
		if err != nil {
			return nil, err
		}
		s.audit.Write(ctx, audit.Entry{UserID: id, Action: "auth.login", Target: username, Err: errors.New("bad password")})
		return nil, errBadCredentials
	}

	// Rotate: any session the browser already holds is revoked before a new one is issued.
	if old, err := r.Cookie(s.cookieName); err == nil && old.Value != "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id_hash = ? AND revoked_at IS NULL`, now, hashToken(old.Value))
	}
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	expires := now.Add(s.cfg.SessionAbsoluteTTL)
	err = db.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET failed_attempts = 0, locked_until = NULL, last_login_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO sessions (id_hash, user_id, created_at, last_seen_at, expires_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			hashToken(tok), id, now, now, expires, meta.IP, meta.UserAgent)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.setCookie(w, tok, expires)
	s.audit.Write(ctx, audit.Entry{UserID: id, Action: "auth.login", Target: username})
	return &UserView{ID: id, Username: username, DisplayName: displayName}, nil
}

func (s *Service) Logout(ctx context.Context, w http.ResponseWriter) {
	if u, ok := reqctx.UserFrom(ctx); ok {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id_hash = ? AND revoked_at IS NULL`, s.now().UTC(), u.SessionHash)
		s.audit.Write(ctx, audit.Entry{UserID: u.ID, Action: "auth.logout"})
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (s *Service) setCookie(w http.ResponseWriter, tok string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    tok,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

type RegisterInput struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

// CreateUser creates a user plus an empty CNY account. Used by registration and seeding.
func CreateUser(ctx context.Context, d *sql.DB, in RegisterInput) (uint64, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !validUsername(in.Username) {
		return 0, apperr.Bad("用户名需为 3-32 位字母、数字或下划线")
	}
	if n := utf8.RuneCountInString(in.DisplayName); n < 1 || n > 32 {
		return 0, apperr.Bad("姓名长度需为 1-32 个字符")
	}
	if err := ValidatePasswordPolicy(in.Password); err != nil {
		return 0, apperr.Bad("%s", err.Error())
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return 0, err
	}
	var id uint64
	err = db.WithTx(ctx, d, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, display_name) VALUES (?, ?, ?)`, in.Username, hash, in.DisplayName)
		if err != nil {
			if db.IsDuplicate(err) {
				return apperr.Conflictf("用户名已被占用")
			}
			return err
		}
		lid, _ := res.LastInsertId()
		id = uint64(lid)
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts (user_id, holder_name) VALUES (?, ?)`, id, in.DisplayName)
		return err
	})
	return id, err
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// ---- HTTP handlers ----

func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.Bad("请求格式错误")
	}
	return nil
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	e, ok := apperr.As(err)
	if !ok {
		slog.ErrorContext(r.Context(), "auth handler failed", "error", err)
		e = apperr.ErrInternal
	}
	httpx.WriteJSON(w, e.Code.HTTPStatus(), map[string]string{"error": e.Message, "code": string(e.Code)})
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var in LoginInput
	if err := decodeStrict(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.Login(r.Context(), w, r, in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	s.Logout(r.Context(), w)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := reqctx.UserFrom(r.Context())
	if !ok {
		writeErr(w, r, apperr.ErrUnauthorized)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName}})
}

func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowRegister {
		writeErr(w, r, apperr.New(apperr.Forbidden, "当前环境未开放注册"))
		return
	}
	if !s.ipLimit.Allow("reg:" + reqctx.MetaFrom(r.Context()).IP) {
		writeErr(w, r, apperr.ErrTooMany)
		return
	}
	var in RegisterInput
	if err := decodeStrict(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	id, err := CreateUser(r.Context(), s.db, in)
	s.audit.Write(r.Context(), audit.Entry{UserID: id, Action: "auth.register", Target: in.Username, Err: err})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": UserView{ID: id, Username: in.Username, DisplayName: in.DisplayName}})
}
