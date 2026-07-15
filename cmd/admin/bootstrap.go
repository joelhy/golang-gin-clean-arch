package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"clean-arch-gin/config"
	"clean-arch-gin/mysqlstore"
	"clean-arch-gin/security"
	"clean-arch-gin/user"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func newBootstrapAdminCommand(deps adminDependencies) *cli.Command {
	return &cli.Command{
		Name:  "bootstrap-admin",
		Usage: "Create the initial administrator account",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "email", Usage: "administrator email", Required: true},
			&cli.StringFlag{Name: "name", Usage: "administrator display name", Required: true},
			&cli.StringFlag{Name: "password", Usage: "administrator password"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := requiredDependency("bootstrapper", deps.Bootstrapper != nil); err != nil {
				return err
			}
			cfg, err := loadActionConfig(deps)
			if err != nil {
				return err
			}
			password := cmd.String("password")
			if !cmd.IsSet("password") {
				if err := requiredDependency("password reader", deps.Password != nil); err != nil {
					return err
				}
				// Password prompting is injectable so tests and non-interactive callers
				// can avoid terminal state while production keeps the secret out of echo.
				password, err = deps.Password.ReadPassword(ctx, cmd)
				if err != nil {
					return err
				}
			}
			account, err := deps.Bootstrapper.BootstrapAdmin(ctx, cfg, user.RegisterInput{
				Email:    cmd.String("email"),
				Name:     cmd.String("name"),
				Password: password,
			})
			if err != nil {
				return err
			}
			return writeCommandLine(cmd, "bootstrap admin complete: %s", account.Email)
		},
	}
}

type terminalPasswordReader struct{}

func (terminalPasswordReader) ReadPassword(ctx context.Context, cmd *cli.Command) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if file, ok := cmd.Root().Reader.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		_, _ = fmt.Fprint(cmd.Root().ErrWriter, "Password: ")
		password, err := term.ReadPassword(int(file.Fd()))
		_, _ = fmt.Fprintln(cmd.Root().ErrWriter)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimRight(string(password), "\r\n"), nil
	}
	if cmd.Root().Reader != nil {
		line, err := bufio.NewReader(cmd.Root().Reader).ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	return "", errors.New("password prompt requires an interactive terminal or injected reader")
}

type bootstrapRuntime struct {
	Logger *slog.Logger
}

func (r bootstrapRuntime) BootstrapAdmin(ctx context.Context, cfg config.Config, input user.RegisterInput) (*user.User, error) {
	db, closeDB, err := mysqlstore.Open(ctx, cfg.DB, r.Logger)
	if err != nil {
		return nil, err
	}
	var result *user.User
	runErr := func() error {
		passwords, err := security.NewPasswordManager(security.PasswordConfig{
			Memory:      cfg.Password.Memory,
			Iterations:  cfg.Password.Iterations,
			Parallelism: cfg.Password.Parallelism,
			SaltLength:  cfg.Password.SaltLength,
			KeyLength:   cfg.Password.KeyLength,
		}, rand.Reader)
		if err != nil {
			return err
		}
		store, err := mysqlstore.NewUserStore(db)
		if err != nil {
			return err
		}
		users, err := user.NewService(store, passwords, user.SystemClock{})
		if err != nil {
			return err
		}
		result, err = users.BootstrapAdmin(ctx, input)
		return err
	}()
	return result, errors.Join(runErr, closeDB())
}
