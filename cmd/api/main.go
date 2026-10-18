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

	"github.com/Bukho127/CampusHub_server/internal/auth"
	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/database"
	"github.com/Bukho127/CampusHub_server/internal/media"
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
	tokens, err := auth.NewTokens(settings.JWTSecret)
	if err != nil {
		return err
	}

	authentication, err := auth.NewService(auth.NewPostgresRepository(pool), tokens)
	if err != nil {
		return err
	}

	var uploads *media.Service
	if settings.Media.Enabled {
		storage, err := media.NewAzureStorage(media.BlobConfig{
			AccountName:   settings.Media.AccountName,
			AccountKey:    settings.Media.AccountKey,
			ContainerName: settings.Media.ContainerName,
			ServiceURL:    settings.Media.BlobURL,
		}, nil)
		if err != nil {
			return err
		}

		processor, err := media.NewFFmpeg(settings.Media.FFmpegPath)
		if err != nil {
			return errors.New("media requires FFmpeg; install it or set FFMPEG_PATH to the executable")
		}
		uploads = media.NewService(media.NewPostgresRepository(pool), storage, processor, settings.Media.CompressionThreshold)
	} else {
		logger.Info("media disabled; configure Azure Blob Storage and set MEDIA_ENABLED=true to enable uploads")
	}

	httpServer := &http.Server{
		Addr:              settings.Address,
		Handler:           server.NewRouter(service, authentication, uploads, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
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
