set dotenv-load := true

app_name := "clean-arch-gin"
server_bin := "bin/server"
admin_bin := "bin/admin"

default:
    @just --list

fmt:
    gofmt -w $(git ls-files '*.go')

test:
    go test ./...

test-race:
    go test -race ./...

test-integration:
    go test -tags=integration ./...

generate:
    go generate ./...

wire:
    go tool wire ./cmd/server ./cmd/admin

migrate-up:
    go run ./cmd/admin migrate up

migrate-down steps:
    go run ./cmd/admin migrate down --steps {{steps}} --confirm

bootstrap-admin email name:
    go run ./cmd/admin bootstrap-admin --email {{email}} --name "{{name}}"

run:
    go run ./cmd/server

build:
    mkdir -p bin
    go build -o {{server_bin}} ./cmd/server
    go build -o {{admin_bin}} ./cmd/admin

docker-build:
    docker build -t {{app_name}}:test .
