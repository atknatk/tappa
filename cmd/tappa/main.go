// Command tappa is the Tappa server: a single binary serving the employee tap
// page, the admin dashboard and the JSON API.
//
// This file does wiring only — no business rules. See CLAUDE.md §3.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The pool is built with a bounded startup context: an unreachable database
	// must fail the boot, not hang it.
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDial()
	data, err := db.New(dialCtx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()

	sessions, err := session.New(data, cfg)
	if err != nil {
		return err
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		return err
	}
	trail, err := audit.New(data)
	if err != nil {
		return err
	}
	activation, err := handler.NewActivation(invites, sessions, trail, cfg, slog.Default())
	if err != nil {
		return err
	}

	// The tap screen (M5-04). It is given the NON-ADVANCING half of internal/sun
	// on purpose: sun.Verifier offers both entry points, but handler.Tap's
	// consumer-side interface names only PreviewWithoutReplayProtection, so the
	// page cannot spend a chip's counter even by accident. The atomic advance
	// belongs to POST /api/checkin (M5-05).
	directory, err := tenant.NewDirectory(data)
	if err != nil {
		return err
	}
	tap, err := handler.NewTap(sun.NewVerifier(data, cfg.TagKEK), directory, sessions, trail, cfg, slog.Default())
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpx.NewRouter(cfg, activation, tap),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Drain in-flight taps before exiting: a dropped request is a lost
		// attendance record, and records are never lost (CLAUDE.md §4).
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
