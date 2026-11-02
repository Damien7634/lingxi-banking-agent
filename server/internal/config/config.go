// Package config loads and validates process configuration from the environment.
// Missing or weak security-critical settings abort startup instead of silently
// falling back to insecure defaults.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env        string // development | production
	ListenAddr string

	DatabaseDSN string

	// AllowedOrigins is the CSRF allowlist for state-changing requests.
	AllowedOrigins []string
	// CookieSecure marks the session cookie Secure (forced on in production).
	CookieSecure bool
	// TrustProxy makes X-Forwarded-For authoritative for client IP. Only enable
	// behind a reverse proxy that overwrites the header.
	TrustProxy bool

	SessionIdleTTL     time.Duration
	SessionAbsoluteTTL time.Duration

	Location *time.Location

	// Transfer limits (cents).
	SingleTransferLimitCents int64
	DailyTransferLimitCents  int64

	// LLM (OpenAI-compatible). Optional: the agent endpoint returns 503 when unset.
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string

	// AllowRegister enables self-service sign-up (off in production by default).
	AllowRegister bool
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

// Load reads .env (if present, without overriding real env vars) then the environment.
func Load() (*Config, error) {
	loadDotEnv(".env")
	loadDotEnv("../.env")

	c := &Config{
		Env:                      get("APP_ENV", "development"),
		ListenAddr:               get("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabaseDSN:              os.Getenv("DATABASE_DSN"),
		AllowedOrigins:           splitList(get("ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000")),
		CookieSecure:             getBool("COOKIE_SECURE", false),
		TrustProxy:               getBool("TRUST_PROXY", false),
		SessionIdleTTL:           getDuration("SESSION_IDLE_TTL", 30*time.Minute),
		SessionAbsoluteTTL:       getDuration("SESSION_ABSOLUTE_TTL", 12*time.Hour),
		SingleTransferLimitCents: getInt64("SINGLE_TRANSFER_LIMIT_CENTS", 100_000_000), // ¥100 万
		DailyTransferLimitCents:  getInt64("DAILY_TRANSFER_LIMIT_CENTS", 200_000_000),  // ¥200 万
		LLMBaseURL:               strings.TrimRight(get("LLM_BASE_URL", "https://api.deepseek.com"), "/"),
		LLMAPIKey:                os.Getenv("LLM_API_KEY"),
		LLMModel:                 get("LLM_MODEL", "deepseek-chat"),
		AllowRegister:            getBool("ALLOW_REGISTER", false),
	}

	loc, err := time.LoadLocation(get("APP_TIMEZONE", "Asia/Shanghai"))
	if err != nil {
		return nil, fmt.Errorf("APP_TIMEZONE: %w", err)
	}
	c.Location = loc

	if c.DatabaseDSN == "" {
		return nil, errors.New("DATABASE_DSN is required, e.g. user:pass@tcp(127.0.0.1:3306)/lingxi")
	}
	if c.IsProduction() {
		c.CookieSecure = true
		c.AllowRegister = getBool("ALLOW_REGISTER", false)
		for _, o := range c.AllowedOrigins {
			if !strings.HasPrefix(o, "https://") {
				return nil, fmt.Errorf("ALLOWED_ORIGINS must be https in production, got %q", o)
			}
		}
	}
	if len(c.AllowedOrigins) == 0 {
		return nil, errors.New("ALLOWED_ORIGINS must not be empty")
	}
	if c.SingleTransferLimitCents <= 0 || c.DailyTransferLimitCents < c.SingleTransferLimitCents {
		return nil, errors.New("transfer limits invalid: need 0 < single <= daily")
	}
	return c, nil
}

func get(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getBool(k string, def bool) bool {
	v, err := strconv.ParseBool(get(k, strconv.FormatBool(def)))
	if err != nil {
		return def
	}
	return v
}

func getInt64(k string, def int64) int64 {
	v, err := strconv.ParseInt(get(k, ""), 10, 64)
	if err != nil {
		return def
	}
	return v
}

func getDuration(k string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(get(k, ""))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.TrimRight(p, "/"))
		}
	}
	return out
}

// loadDotEnv is a minimal KEY=VALUE reader; real environment variables win.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if i := strings.Index(v, " #"); i >= 0 {
			v = v[:i]
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
