package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // Windows hosts have no system zoneinfo

	"github.com/go-chi/chi/v5"

	"lingxi/server/internal/agent"
	"lingxi/server/internal/apperr"
	"lingxi/server/internal/audit"
	"lingxi/server/internal/auth"
	"lingxi/server/internal/config"
	"lingxi/server/internal/db"
	"lingxi/server/internal/httpx"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/service"
	"lingxi/server/internal/trpc"
	"lingxi/server/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(ctx, d); err != nil {
		return err
	}

	aud := audit.New(d)
	authSvc := auth.NewService(d, cfg, aud)
	svc := service.New(d, cfg, aud)
	agentSvc := agent.New(svc, cfg)

	mutationLimiter := httpx.NewLimiter(60, 15) // per user
	rpc := trpc.NewRouter(trpc.Hooks{
		Authenticated: func(ctx context.Context) bool { _, ok := reqctx.UserFrom(ctx); return ok },
		BeforeMutation: func(ctx context.Context, path string) error {
			uid, _ := reqctx.UserID(ctx)
			if !mutationLimiter.Allow("m:" + uintStr(uid)) {
				return apperr.ErrTooMany
			}
			return nil
		},
		AfterMutation: func(ctx context.Context, path string, input json.RawMessage, err error) {
			uid, _ := reqctx.UserID(ctx)
			aud.Write(ctx, audit.Entry{UserID: uid, Action: path, Params: input, Err: err})
		},
	})
	svc.Register(rpc)

	r := chi.NewRouter()
	r.Use(httpx.RequestMeta(cfg.TrustProxy), httpx.Recover, httpx.SecurityHeaders(cfg.IsProduction()), authSvc.Middleware)

	r.Route("/api", func(api chi.Router) {
		api.Use(httpx.IPRateLimit(httpx.NewLimiter(600, 120)), httpx.OriginGuard(cfg.AllowedOrigins))
		api.With(httpx.BodyLimit(8<<10), httpx.RequireUTF8).Post("/auth/login", authSvc.HandleLogin)
		api.With(httpx.BodyLimit(8<<10), httpx.RequireUTF8).Post("/auth/register", authSvc.HandleRegister)
		api.Post("/auth/logout", authSvc.HandleLogout)
		api.Get("/auth/me", authSvc.HandleMe)
		api.With(httpx.BodyLimit(1<<20)).Handle("/trpc/*", rpc)
		api.With(httpx.BodyLimit(256<<10), httpx.RequireUTF8).Post("/agent/chat", agentSvc.HandleChat)
		api.With(httpx.BodyLimit(64<<10)).Post("/log", handleClientLog)
		api.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "Not Found"})
		})
	})
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		r.Handle("/*", spa(dir))
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      3 * time.Minute, // agent SSE streams can be long
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	worker.Start(ctx, svc)

	errc := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.ListenAddr, "env", cfg.Env)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	slog.Info("shutting down")
	return srv.Shutdown(shutdownCtx)
}

// spa serves the built frontend and falls back to index.html for client routes.
func spa(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			if !strings.Contains(filepath.Base(r.URL.Path), ".") {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

func uintStr(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
