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

	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/database"
	"github.com/Bukho127/CampusHub_server/internal/server"
	"github.com/Bukho127/CampusHub_server/internal/users"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pool, err := database.Open(connectCtx, settings.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()

	service := users.NewService(users.NewPostgresRepository(pool))
	httpServer := &http.Server{
		Addr:              settings.Address,
		Handler:           server.NewRouter(service, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server starting", "address", settings.Address)
		serveErrors <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
			return err
		}
		logger.Info("HTTP server stopped")
		return nil
	}
}
