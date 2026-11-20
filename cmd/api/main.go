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

	"alisaai/internal/config"
	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
	"alisaai/internal/service"
	"alisaai/internal/transport/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tickTickClient := ticktick.NewClient(cfg.TickTickToken, cfg.TickTickTimeout)
	tickTickService := service.NewTickTick(tickTickClient, cfg.TickTickTimeout)
	gigaChatClient, err := gigachat.NewClient(gigachat.Options{
		AuthKey: cfg.GigaChatAuthKey, Scope: cfg.GigaChatScope,
		BaseURL: cfg.GigaChatBaseURL, AuthURL: cfg.GigaChatAuthURL,
		CAFile: cfg.GigaChatCAFile, Timeout: cfg.GigaChatTimeout,
	})
	if err != nil {
		return err
	}
	gigaChatService := service.NewGigaChat(gigaChatClient, tickTickService, cfg.GigaChatModel, cfg.GigaChatTimeout)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(gigaChatService),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server", "addr", cfg.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		return err
	case <-ctx.Done():
		stop()
	}

	logger.Info("shutting down HTTP server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	if err := <-serverErrors; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
