# Clean Architecture Gin API

Domain-first Go API using Gin, MySQL, GORM Gen, versioned SQL migrations, Wire, and explicit runtime commands.

The current application implements users, RBAC, authentication sessions, products, orders, inventory adjustments, admin statistics, and a uniform JSON response contract.

## Prerequisites

- Go 1.25+
- MySQL 8.4 for local development or Docker for containerized development
- Just, optional but recommended for common commands
- Wire is pinned through the Go `tool` directive and can be run with `go tool wire`

## Configuration

Copy `env.example` to `.env` for local commands that load dotenv through `just`, or export the variables in your shell. The server and admin command read only `APP_*` variables through the standard library.

Generate a JWT key before running anything that loads configuration:

```bash
openssl rand -base64 48
```

Set at minimum:

```bash
APP_JWT_KEY=<generated value>
APP_DB_HOST=127.0.0.1
APP_DB_PORT=3306
APP_DB_USER=app
APP_DB_PASSWORD=<database password>
APP_DB_NAME=clean_arch
```

Production rejects placeholder JWT keys and requires a database password.

## Database

Run migrations explicitly. Service startup never runs `AutoMigrate`.

```bash
just migrate-up
just migrate-down 1
```

The down command requires both a positive step count and `--confirm`; the just recipe supplies the confirmation flag.

## Bootstrap Admin

Create the first administrator after migrations:

```bash
just bootstrap-admin admin@example.com "Admin User"
```

If `--password` is omitted, the command prompts without echo when attached to a terminal. Avoid passing passwords on the command line in shared environments because process arguments can be visible to other users.

## Run

```bash
just run
```

Health endpoints:

```text
GET /health/live
GET /health/ready
```

## Tests And Generation

```bash
just fmt
just generate
just test
just test-race
just test-integration
go vet ./...
```

`go generate ./...` refreshes GORM Gen query code and Wire injectors. Generated code is committed and should be reproducible.

## Docker

Create `.env` with `APP_JWT_KEY`, `APP_DB_PASSWORD`, and `MYSQL_ROOT_PASSWORD`, then start MySQL and the app:

```bash
docker compose up --build mysql app
```

Run migrations as an explicit one-shot service:

```bash
docker compose --profile tools run --rm migrate
```

Build the server image:

```bash
just docker-build
```

## Documentation

- [Architecture](docs/architecture.md)
- [API](docs/api.md)
- [Error Codes](docs/error-codes.md)
