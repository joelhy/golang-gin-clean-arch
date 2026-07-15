package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"clean-arch-gin/config"
	"clean-arch-gin/migrations"
	"clean-arch-gin/user"
	"github.com/urfave/cli/v3"
)

type migrationRunner interface {
	RunMigrations(context.Context, config.Config, migrations.Direction, uint) error
}

type adminBootstrapper interface {
	BootstrapAdmin(context.Context, config.Config, user.RegisterInput) (*user.User, error)
}

type passwordReader interface {
	ReadPassword(context.Context, *cli.Command) (string, error)
}

type adminDependencies struct {
	LoadConfig   func() (config.Config, error)
	Migrator     migrationRunner
	Bootstrapper adminBootstrapper
	Password     passwordReader
}

func NewRootCommand(deps adminDependencies) *cli.Command {
	deps = deps.withDefaults()
	return &cli.Command{
		Name:      "admin",
		Usage:     "Run administrative maintenance commands",
		Writer:    io.Discard,
		ErrWriter: io.Discard,
		ExitErrHandler: func(context.Context, *cli.Command, error) {
			// Commands return errors to main/tests; process exit is centralized in main.
		},
		Commands: []*cli.Command{
			newMigrateCommand(deps),
			newBootstrapAdminCommand(deps),
		},
	}
}

func (d adminDependencies) withDefaults() adminDependencies {
	if d.LoadConfig == nil {
		d.LoadConfig = config.Load
	}
	if d.Migrator == nil {
		d.Migrator = migrationRuntime{}
	}
	if d.Bootstrapper == nil {
		d.Bootstrapper = bootstrapRuntime{}
	}
	if d.Password == nil {
		d.Password = terminalPasswordReader{}
	}
	return d
}

func loadActionConfig(deps adminDependencies) (config.Config, error) {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return config.Config{}, fmt.Errorf("load configuration: %w", err)
	}
	return cfg, nil
}

func writeCommandLine(cmd *cli.Command, format string, args ...any) error {
	_, err := fmt.Fprintf(cmd.Root().Writer, format+"\n", args...)
	return err
}

func requiredDependency(name string, ok bool) error {
	if ok {
		return nil
	}
	return errors.New(name + " dependency is required")
}

func provideAdminDependencies(migrator migrationRunner, bootstrapper adminBootstrapper, password passwordReader) adminDependencies {
	return adminDependencies{
		LoadConfig:   config.Load,
		Migrator:     migrator,
		Bootstrapper: bootstrapper,
		Password:     password,
	}
}

func provideMigrationRunner() migrationRunner {
	return migrationRuntime{}
}

func provideBootstrapper(logger *slog.Logger) adminBootstrapper {
	return bootstrapRuntime{Logger: logger}
}

func providePasswordReader() passwordReader {
	return terminalPasswordReader{}
}
