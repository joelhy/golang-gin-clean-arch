package main

import (
	"context"
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps, err := initializeAdminDependencies(logger)
	if err != nil {
		logger.Error("initialize admin command", slog.Any("error", err))
		os.Exit(1)
	}
	root := NewRootCommand(deps)
	root.Reader = os.Stdin
	root.Writer = os.Stdout
	root.ErrWriter = os.Stderr
	if err := root.Run(context.Background(), os.Args); err != nil {
		logger.Error("admin command failed", slog.Any("error", err))
		os.Exit(1)
	}
}
