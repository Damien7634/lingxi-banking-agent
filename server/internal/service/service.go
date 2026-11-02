// Package service implements every business procedure exposed over tRPC.
// Each domain file registers its procedures in Register; all of them take the
// user id from the session (reqctx.UserID), never from input.
package service

import (
	"context"
	"database/sql"
	"time"

	"lingxi/server/internal/audit"
	"lingxi/server/internal/config"
	"lingxi/server/internal/db"
	"lingxi/server/internal/trpc"
)

type Service struct {
	DB    *sql.DB
	Cfg   *config.Config
	Audit *audit.Logger
	Now   func() time.Time
}

func New(d *sql.DB, cfg *config.Config, a *audit.Logger) *Service {
	return &Service{DB: d, Cfg: cfg, Audit: a, Now: time.Now}
}

func (s *Service) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return db.WithTx(ctx, s.DB, fn)
}

// Register wires every procedure the React client calls.
func (s *Service) Register(r *trpc.Router) {
	r.PublicQuery("ping", trpc.NoInput(func(ctx context.Context) (map[string]any, error) {
		return map[string]any{"ok": true, "ts": s.Now().UnixMilli()}, nil
	}))
	s.registerAccount(r)
	s.registerTransfer(r)
	s.registerBill(r)
	s.registerCard(r)
	s.registerScenario(r)
	s.registerWealth(r)
	s.registerFX(r)
	s.registerSCF(r)
	s.registerTrade(r)
}

// local returns t in the bank's business timezone (month/day boundaries).
func (s *Service) local(t time.Time) time.Time { return t.In(s.Cfg.Location) }
