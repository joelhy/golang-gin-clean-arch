package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"clean-arch-gin/config"
	"clean-arch-gin/migrations"
	"clean-arch-gin/user"
	"github.com/urfave/cli/v3"
)

func TestRootHelpWritesToCommandWriter(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCommand(fakeAdminDependencies().adminDependencies)
	root.Writer = &stdout

	if err := root.Run(t.Context(), []string{"admin", "--help"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"migrate", "bootstrap-admin"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
	}
}

func TestUnknownCommandReturnsError(t *testing.T) {
	root := NewRootCommand(fakeAdminDependencies().adminDependencies)

	err := root.Run(t.Context(), []string{"admin", "missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Run() error = %v, want unknown command mentioning name", err)
	}
}

func TestMigrateUpRunsAllPendingMigrations(t *testing.T) {
	deps := fakeAdminDependencies()
	var stdout bytes.Buffer
	root := NewRootCommand(deps.adminDependencies)
	root.Writer = &stdout

	if err := root.Run(t.Context(), []string{"admin", "migrate", "up"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if deps.loadCalls != 1 {
		t.Fatalf("loadCalls = %d, want 1", deps.loadCalls)
	}
	if deps.migrator.direction != migrations.Up || deps.migrator.steps != 0 {
		t.Fatalf("migration = (%s, %d), want (%s, 0)", deps.migrator.direction, deps.migrator.steps, migrations.Up)
	}
	if !strings.Contains(stdout.String(), "migrate up complete") {
		t.Fatalf("stdout = %q, want completion message", stdout.String())
	}
}

func TestMigrateDownRequiresPositiveSteps(t *testing.T) {
	root := NewRootCommand(fakeAdminDependencies().adminDependencies)

	err := root.Run(t.Context(), []string{"admin", "migrate", "down", "--confirm"})
	if err == nil || !strings.Contains(err.Error(), "--steps") {
		t.Fatalf("Run() error = %v, want --steps validation", err)
	}
}

func TestMigrateDownRequiresConfirmation(t *testing.T) {
	root := NewRootCommand(fakeAdminDependencies().adminDependencies)

	err := root.Run(t.Context(), []string{"admin", "migrate", "down", "--steps", "1"})
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("Run() error = %v, want --confirm validation", err)
	}
}

func TestMigrateDownRunsConfirmedSteps(t *testing.T) {
	deps := fakeAdminDependencies()
	root := NewRootCommand(deps.adminDependencies)

	if err := root.Run(t.Context(), []string{"admin", "migrate", "down", "--steps", "2", "--confirm"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if deps.migrator.direction != migrations.Down || deps.migrator.steps != 2 {
		t.Fatalf("migration = (%s, %d), want (%s, 2)", deps.migrator.direction, deps.migrator.steps, migrations.Down)
	}
}

func TestMigrateUsesStdlibConfigLoader(t *testing.T) {
	t.Setenv("APP_ENVIRONMENT", "test")
	t.Setenv("APP_JWT_KEY", strings.Repeat("k", 32))
	t.Setenv("APP_HTTP_ADDRESS", "127.0.0.1:8081")

	deps := fakeAdminDependencies()
	deps.LoadConfig = config.Load
	root := NewRootCommand(deps.adminDependencies)

	if err := root.Run(t.Context(), []string{"admin", "migrate", "up"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if deps.migrator.cfg.HTTP.Address != "127.0.0.1:8081" {
		t.Fatalf("cfg.HTTP.Address = %q, want APP_HTTP_ADDRESS", deps.migrator.cfg.HTTP.Address)
	}
}

func TestBootstrapAdminRequiresEmailAndName(t *testing.T) {
	root := NewRootCommand(fakeAdminDependencies().adminDependencies)

	err := root.Run(t.Context(), []string{"admin", "bootstrap-admin", "--name", "Root", "--password", "correct horse"})
	if err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("Run() error = %v, want missing email", err)
	}
}

func TestBootstrapAdminUsesPasswordFlagWithoutPromptOrLeak(t *testing.T) {
	deps := fakeAdminDependencies()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(deps.adminDependencies)
	root.Writer = &stdout
	root.ErrWriter = &stderr

	err := root.Run(t.Context(), []string{
		"admin", "bootstrap-admin",
		"--email", "root@example.com",
		"--name", "Root",
		"--password", "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if deps.prompt.calls != 0 {
		t.Fatalf("prompt calls = %d, want 0 when --password is provided", deps.prompt.calls)
	}
	if deps.bootstrapper.input.Password != "correct horse battery staple" {
		t.Fatalf("password = %q, want flag value", deps.bootstrapper.input.Password)
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, deps.bootstrapper.input.Password) {
		t.Fatalf("output leaked password: %q", combined)
	}
}

func TestBootstrapAdminPromptsWhenPasswordMissingWithoutLeaking(t *testing.T) {
	deps := fakeAdminDependencies()
	deps.prompt.password = "prompted secure password"
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(deps.adminDependencies)
	root.Writer = &stdout
	root.ErrWriter = &stderr

	err := root.Run(t.Context(), []string{
		"admin", "bootstrap-admin",
		"--email", "root@example.com",
		"--name", "Root",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if deps.prompt.calls != 1 {
		t.Fatalf("prompt calls = %d, want 1", deps.prompt.calls)
	}
	if deps.bootstrapper.input.Password != deps.prompt.password {
		t.Fatalf("password = %q, want prompted value", deps.bootstrapper.input.Password)
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, deps.prompt.password) {
		t.Fatalf("output leaked prompted password: %q", combined)
	}
}

func TestBootstrapAdminReturnsDuplicateEmailError(t *testing.T) {
	deps := fakeAdminDependencies()
	deps.bootstrapper.err = user.ErrEmailExists
	root := NewRootCommand(deps.adminDependencies)

	err := root.Run(t.Context(), []string{
		"admin", "bootstrap-admin",
		"--email", "root@example.com",
		"--name", "Root",
		"--password", "correct horse battery staple",
	})
	if !errors.Is(err, user.ErrEmailExists) {
		t.Fatalf("Run() error = %v, want ErrEmailExists", err)
	}
}

type fakeAdminDeps struct {
	adminDependencies
	loadCalls    int
	migrator     *fakeMigrator
	bootstrapper *fakeBootstrapper
	prompt       *fakePasswordPrompt
}

func fakeAdminDependencies() *fakeAdminDeps {
	deps := &fakeAdminDeps{
		migrator:     &fakeMigrator{},
		bootstrapper: &fakeBootstrapper{},
		prompt:       &fakePasswordPrompt{password: "prompted secure password"},
	}
	deps.adminDependencies = adminDependencies{
		LoadConfig: func() (config.Config, error) {
			deps.loadCalls++
			return config.Config{Environment: "test"}, nil
		},
		Migrator:     deps.migrator,
		Bootstrapper: deps.bootstrapper,
		Password:     deps.prompt,
	}
	return deps
}

type fakeMigrator struct {
	cfg       config.Config
	direction migrations.Direction
	steps     uint
	err       error
}

func (m *fakeMigrator) RunMigrations(ctx context.Context, cfg config.Config, direction migrations.Direction, steps uint) error {
	m.cfg = cfg
	m.direction = direction
	m.steps = steps
	return m.err
}

type fakeBootstrapper struct {
	cfg   config.Config
	input user.RegisterInput
	err   error
}

func (b *fakeBootstrapper) BootstrapAdmin(ctx context.Context, cfg config.Config, input user.RegisterInput) (*user.User, error) {
	b.cfg = cfg
	b.input = input
	if b.err != nil {
		return nil, b.err
	}
	return &user.User{ID: 42, Email: input.Email, Name: input.Name}, nil
}

type fakePasswordPrompt struct {
	password string
	calls    int
	err      error
}

func (p *fakePasswordPrompt) ReadPassword(ctx context.Context, cmd *cli.Command) (string, error) {
	p.calls++
	if p.err != nil {
		return "", p.err
	}
	return p.password, nil
}
