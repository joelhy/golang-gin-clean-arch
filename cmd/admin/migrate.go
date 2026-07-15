package main

import (
	"context"
	"database/sql"
	"errors"

	"clean-arch-gin/config"
	"clean-arch-gin/migrations"
	"clean-arch-gin/mysqlstore"
	"github.com/urfave/cli/v3"
)

func newMigrateCommand(deps adminDependencies) *cli.Command {
	return &cli.Command{
		Name:  "migrate",
		Usage: "Apply or revert database migrations",
		Commands: []*cli.Command{
			{
				Name:  "up",
				Usage: "Apply all pending migrations",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := requiredDependency("migrator", deps.Migrator != nil); err != nil {
						return err
					}
					cfg, err := loadActionConfig(deps)
					if err != nil {
						return err
					}
					if err := deps.Migrator.RunMigrations(ctx, cfg, migrations.Up, 0); err != nil {
						return err
					}
					return writeCommandLine(cmd, "migrate up complete")
				},
			},
			{
				Name:  "down",
				Usage: "Revert a bounded number of migrations",
				Flags: []cli.Flag{
					&cli.UintFlag{Name: "steps", Usage: "number of versions to revert"},
					&cli.BoolFlag{Name: "confirm", Usage: "confirm destructive migration rollback"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					steps := cmd.Uint("steps")
					if steps == 0 {
						return errors.New("migrate down requires --steps greater than zero")
					}
					// Rollbacks are intentionally a two-key operation: a bounded step
					// count prevents full teardown, and --confirm keeps accidental invocations inert.
					if !cmd.Bool("confirm") {
						return errors.New("migrate down requires --confirm")
					}
					if err := requiredDependency("migrator", deps.Migrator != nil); err != nil {
						return err
					}
					cfg, err := loadActionConfig(deps)
					if err != nil {
						return err
					}
					if err := deps.Migrator.RunMigrations(ctx, cfg, migrations.Down, steps); err != nil {
						return err
					}
					return writeCommandLine(cmd, "migrate down complete")
				},
			},
		},
	}
}

type migrationRuntime struct{}

func (migrationRuntime) RunMigrations(ctx context.Context, cfg config.Config, direction migrations.Direction, steps uint) error {
	db, closeDB, err := mysqlstore.OpenMigrations(ctx, cfg.DB)
	if err != nil {
		return err
	}
	runErr := runMigration(ctx, db, direction, steps)
	// Closing the migration pool is part of command correctness; errors.Join keeps
	// the migration failure and close failure independently inspectable by callers.
	return errors.Join(runErr, closeDB())
}

func runMigration(ctx context.Context, db *sql.DB, direction migrations.Direction, steps uint) error {
	return migrations.Run(ctx, db, direction, steps)
}
