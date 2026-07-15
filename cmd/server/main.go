package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"clean-arch-gin/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := listen(cfg)
	if err != nil {
		logger.Error("listen", slog.Any("error", err))
		os.Exit(1)
	}

	app, err := initializeRuntime(ctx, cfg, logger, listener)
	if err != nil {
		// Runtime construction happens after the port is reserved so bind failures
		// cannot leak a database pool; close the listener if later providers fail.
		_ = listener.Close()
		logger.Error("initialize server", slog.Any("error", err))
		os.Exit(1)
	}
	if err := run(ctx, app); err != nil {
		logger.Error("server stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}
