// Package worker runs periodic jobs: scheduled transfers, FX settlement,
// scenario progression and loan overdue marking. Every job is idempotent and
// row-locked, so running several instances is safe.
package worker

import (
	"context"
	"log/slog"
	"time"

	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/service"
)

type job struct {
	name  string
	every time.Duration
	run   func(ctx context.Context) error
}

func Start(ctx context.Context, svc *service.Service) {
	jobs := []job{
		{"scheduledTransfers", 30 * time.Second, func(ctx context.Context) error { _, err := svc.RunDueScheduled(ctx); return err }},
		{"fxSettlement", time.Minute, func(ctx context.Context) error { _, err := svc.SettleExpiredFX(ctx); return err }},
		{"scenarios", time.Minute, func(ctx context.Context) error { _, err := svc.TickScenarios(ctx, 0); return err }},
		{"loanOverdue", 10 * time.Minute, func(ctx context.Context) error { _, err := svc.MarkOverdueLoans(ctx); return err }},
	}
	for _, j := range jobs {
		go loop(ctx, j)
	}
}

func loop(ctx context.Context, j job) {
	t := time.NewTicker(j.every)
	defer t.Stop()
	for {
		runOnce(ctx, j)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func runOnce(ctx context.Context, j job) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("worker panic", "job", j.name, "panic", r)
		}
	}()
	jctx, cancel := context.WithTimeout(reqctx.WithMeta(ctx, reqctx.Meta{RequestID: "worker-" + j.name, IP: "internal"}), j.every)
	defer cancel()
	if err := j.run(jctx); err != nil && ctx.Err() == nil {
		slog.Error("worker job failed", "job", j.name, "error", err)
	}
}
