// Package migrations embeds and runs the application's versioned MySQL schema.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Files is the immutable migration source shipped with every application binary.
//
//go:embed *.sql
var Files embed.FS

// Direction selects whether Run applies or reverts migration versions.
type Direction string

const (
	// Up applies newer schema versions.
	Up Direction = "up"
	// Down reverts already-applied schema versions.
	Down Direction = "down"
)

// Run applies a bounded number of migrations, or all pending up migrations when steps is zero.
func Run(ctx context.Context, db *sql.DB, direction Direction, steps uint) error {
	if ctx == nil {
		return errors.New("run migration: context is required")
	}
	if db == nil {
		return errors.New("run migration: database is required")
	}
	if direction != Up && direction != Down {
		return fmt.Errorf("run migration: unsupported direction %q", direction)
	}
	if direction == Down && steps == 0 {
		return errors.New("run migration down: steps must be greater than zero")
	}
	maxInt := int(^uint(0) >> 1)
	if steps > uint(maxInt) {
		return fmt.Errorf("run migration %s: steps %d exceed platform limit", direction, steps)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("run migration %s (steps=%d): %w", direction, steps, err)
	}

	sourceDriver, err := iofs.New(Files, ".")
	if err != nil {
		return fmt.Errorf("run migration %s (steps=%d): create embedded source: %w", direction, steps, err)
	}

	// A dedicated connection is required because MySQL advisory locks must be acquired
	// and released on the same connection. Closing the migrate driver then returns only
	// this connection to the caller-owned pool instead of closing the whole database.
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = sourceDriver.Close()
		return fmt.Errorf("run migration %s (steps=%d): acquire connection: %w", direction, steps, err)
	}
	databaseDriver, err := migratemysql.WithConnection(ctx, conn, &migratemysql.Config{})
	if err != nil {
		_ = conn.Close()
		_ = sourceDriver.Close()
		return fmt.Errorf("run migration %s (steps=%d): initialize mysql driver: %w", direction, steps, err)
	}

	runner, err := migrate.NewWithInstance("iofs", sourceDriver, "mysql", databaseDriver)
	if err != nil {
		_ = databaseDriver.Close()
		_ = sourceDriver.Close()
		return fmt.Errorf("run migration %s (steps=%d): initialize runner: %w", direction, steps, err)
	}

	// golang-migrate exposes cooperative cancellation only between migration versions. The
	// watcher is explicitly joined so every return path leaves no cancellation goroutine.
	stopWatcher := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			select {
			case runner.GracefulStop <- true:
			default:
			}
		case <-stopWatcher:
		}
	}()

	runErr := run(runner, direction, steps)
	close(stopWatcher)
	<-watcherDone
	sourceCloseErr, databaseCloseErr := runner.Close()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("run migration %s (steps=%d): %w", direction, steps, ctxErr)
	}
	if runErr != nil && !errors.Is(runErr, migrate.ErrNoChange) {
		return fmt.Errorf("run migration %s (steps=%d): %w", direction, steps, runErr)
	}
	if closeErr := errors.Join(sourceCloseErr, databaseCloseErr); closeErr != nil {
		return fmt.Errorf("run migration %s (steps=%d): close runner: %w", direction, steps, closeErr)
	}
	return nil
}

func run(runner *migrate.Migrate, direction Direction, steps uint) error {
	if direction == Up && steps == 0 {
		return runner.Up()
	}
	if direction == Up {
		return runner.Steps(int(steps))
	}
	return runner.Steps(-int(steps))
}
