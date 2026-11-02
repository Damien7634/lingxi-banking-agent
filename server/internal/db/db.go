// Package db owns the MySQL pool, transactions and schema migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open connects with settings that matter for money: UTC timestamps, strict
// SQL mode (no silent truncation), and a bounded pool.
func Open(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false // never allow stacked queries
	cfg.InterpolateParams = false
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Params["sql_mode"] = "'STRICT_ALL_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'"
	cfg.Params["transaction_isolation"] = "'REPEATABLE-READ'"
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	d := sql.OpenDB(conn)
	d.SetMaxOpenConns(30)
	d.SetMaxIdleConns(10)
	d.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return d, nil
}

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// WithTx runs fn in a transaction and retries on deadlock / lock-wait timeout.
// fn must be safe to re-run (it is, as long as it only touches the tx).
func WithTx(ctx context.Context, d *sql.DB, fn func(tx *sql.Tx) error) error {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = runTx(ctx, d, fn)
		if err == nil || !isRetryable(err) {
			return err
		}
		slog.WarnContext(ctx, "tx retry", "attempt", attempt, "error", err)
		time.Sleep(time.Duration(attempt*20) * time.Millisecond)
	}
	return err
}

func runTx(ctx context.Context, d *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func isRetryable(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1213 || me.Number == 1205 // deadlock, lock wait timeout
	}
	return false
}

// IsDuplicate reports a unique-key violation.
func IsDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// EscapeLike escapes %, _ and \ so user input is matched literally in LIKE.
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Migrate applies embedded migrations in filename order, once each.
func Migrate(ctx context.Context, d *sql.DB) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(64) PRIMARY KEY,
		applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
	) ENGINE=InnoDB`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var exists int
		err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return err
		}
		// MySQL DDL auto-commits, so statements run one by one; each migration
		// must be written to be re-runnable (IF NOT EXISTS) in case it fails midway.
		for _, stmt := range splitStatements(string(body)) {
			if _, err := d.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration %s: %w\n%s", version, err, stmt)
			}
		}
		if _, err := d.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return err
		}
		slog.Info("migration applied", "version", version)
	}
	return nil
}

func splitStatements(s string) []string {
	var out []string
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "--") || t == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
		if strings.HasSuffix(t, ";") {
			out = append(out, strings.TrimSuffix(strings.TrimSpace(b.String()), ";"))
			b.Reset()
		}
	}
	if rest := strings.TrimSpace(b.String()); rest != "" {
		out = append(out, rest)
	}
	return out
}
