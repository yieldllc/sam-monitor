package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yieldllc/sam-monitor/internal/alert"
	"github.com/yieldllc/sam-monitor/internal/db"
	"github.com/yieldllc/sam-monitor/internal/poller"
	"github.com/yieldllc/sam-monitor/internal/sam"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	pool, err := db.Open(ctx, dsn)
	if err != nil {
		slog.Error("db open", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("db connected + migrated")

	samc := &sam.Client{
		APIKey: os.Getenv("SAM_API_KEY"),
		HTTP:   &http.Client{Timeout: 30 * time.Second},
	}
	if samc.APIKey == "" {
		slog.Warn("SAM_API_KEY is empty — pollers will fail")
	}

	alerter := alert.FromEnv()
	if alerter == nil {
		slog.Warn("no SMTP_HOST — alerting disabled")
	}

	// Opportunity poller — 4h cadence. Initial poll runs on startup.
	pol := &poller.Poller{DB: pool, SAM: samc, Alerter: alerter}
	go runTicker(ctx, "opp-poller", 4*time.Hour, func(c context.Context) {
		if err := pol.PollAll(c); err != nil {
			slog.Warn("opp poll", "err", err)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// runTicker fires fn immediately, then every `interval` until ctx is cancelled.
// fn is called with the parent ctx so it cancels on shutdown.
func runTicker(ctx context.Context, name string, interval time.Duration, fn func(context.Context)) {
	slog.Info("ticker start", "name", name, "interval", interval)
	fn(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("ticker stop", "name", name)
			return
		case <-t.C:
			fn(ctx)
		}
	}
}
