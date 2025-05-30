# 🚀 Clean Architecture Gin API - Justfile
# Modern command runner using Just (https://github.com/casey/just)

# Variables
app_name := "clean-arch-gin"
build_dir := "bin"
main_path := "cmd/main.go"

# Default recipe - show help
default:
    @just --list

# 📖 Show detailed help with descriptions
help:
    @echo "🚀 Clean Architecture Gin API"
    @echo "============================"
    @echo ""
    @echo "📦 Development Commands:"
    @echo "  dev          - Start development server with hot reload"
    @echo "  dev-setup    - Complete development environment setup"
    @echo "  quick-dev    - Quick start for development (deps + gen + dev)"
    @echo ""
    @echo "🏗️  Build Commands:"
    @echo "  build        - Build the application"
    @echo "  build-linux  - Build for Linux (cross-compile)"
    @echo "  prod-build   - Production build with optimizations"
    @echo ""
    @echo "🧪 Testing Commands:"
    @echo "  test         - Run all tests"
    @echo "  test-cov     - Run tests with coverage report"
    @echo "  test-watch   - Run tests in watch mode"
    @echo ""
    @echo "⚙️  Code Generation:"
    @echo "  wire         - Generate dependency injection code"
    @echo "  gen-query    - Generate GORM Gen type-safe queries"
    @echo "  gen-all      - Generate all code (Wire + GORM Gen)"
    @echo ""
    @echo "🗃️  Database Commands:"
    @echo "  setup-db     - Setup database for development"
    @echo "  migrate      - Run database migrations"
    @echo ""
    @echo "🐳 Docker Commands:"
    @echo "  docker-up    - Start Docker services"
    @echo "  docker-down  - Stop Docker services"
    @echo "  docker-build - Build Docker image"
    @echo "  docker-run   - Run application in Docker"
    @echo ""
    @echo "🧹 Utility Commands:"
    @echo "  clean        - Clean build artifacts and generated code"
    @echo "  clean-gen    - Clean only generated code"
    @echo "  fmt          - Format Go code"
    @echo "  lint         - Run linter"
    @echo "  deps         - Download and tidy dependencies"
    @echo "  install-tools - Install required development tools"
    @echo ""
    @echo "📚 Documentation:"
    @echo "  docs         - Generate and serve documentation"

# 📦 Development Commands

# Start development server with hot reload
dev:
    @echo "🚀 Starting development server..."
    go run {{main_path}}

# Complete development environment setup
dev-setup: docker-up migrate gen-all
    @echo "🎉 Development environment setup completed!"
    @echo "💡 You can now run 'just dev' to start the server"

# Quick start for development
quick-dev: deps gen-query dev

# 🏗️ Build Commands

# Build the application
build:
    @echo "🏗️  Building application..."
    @mkdir -p {{build_dir}}
    go build -o {{build_dir}}/{{app_name}} {{main_path}}
    @echo "✅ Build completed: {{build_dir}}/{{app_name}}"

# Build for Linux (cross-compile)
build-linux:
    @echo "🐧 Building for Linux..."
    @mkdir -p {{build_dir}}
    GOOS=linux GOARCH=amd64 go build -o {{build_dir}}/{{app_name}}-linux {{main_path}}
    @echo "✅ Linux build completed: {{build_dir}}/{{app_name}}-linux"

# Production build with optimizations
prod-build: gen-all
    @echo "🚀 Building for production..."
    @mkdir -p {{build_dir}}
    go build -ldflags="-s -w" -o {{build_dir}}/{{app_name}} {{main_path}}
    @echo "✅ Production build completed"

# 🧪 Testing Commands

# Run all tests
test:
    @echo "🧪 Running tests..."
    go test -v ./...

# Run tests with coverage report
test-cov:
    @echo "📊 Running tests with coverage..."
    go test -v -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "📈 Coverage report generated: coverage.html"

# Run tests in watch mode (requires cargo-watch or similar)
test-watch:
    @echo "👀 Running tests in watch mode..."
    @echo "💡 Install 'entr' for file watching: brew install entr (macOS) or apt install entr (Ubuntu)"
    find . -name "*.go" | entr -c go test ./...

# ⚙️ Code Generation Commands

# Generate dependency injection code using Wire
wire:
    @echo "⚙️  Generating Wire dependency injection code..."
    cd internal/di && wire
    @echo "✅ Wire code generated"

# Generate GORM Gen type-safe queries
gen-query:
    @echo "🔧 Generating GORM Gen query code..."
    @echo "📋 Note: Ensure database is running and schema is up to date"
    go run internal/infrastructure/database/gen.go
    @echo "✅ GORM Gen code generated in internal/infrastructure/database/query/"

# Generate all code (Wire + GORM Gen)
gen-all: wire gen-query
    @echo "🎯 All code generation completed!"

# 🗃️ Database Commands

# Setup database for development
setup-db: docker-up
    @echo "🗃️  Setting up database..."
    @echo "⏳ Waiting for database to be ready..."
    sleep 10
    @echo "✅ Database setup completed"

# Run database migrations
migrate:
    @echo "🔄 Running database migrations..."
    @echo "📋 Note: GORM handles auto-migration"
    @echo "✅ Migrations completed"

# 🐳 Docker Commands

# Start Docker services
docker-up:
    @echo "🐳 Starting Docker services..."
    docker-compose up -d
    @echo "✅ Docker services started"

# Stop Docker services
docker-down:
    @echo "🛑 Stopping Docker services..."
    docker-compose down
    @echo "✅ Docker services stopped"

# Build Docker image
docker-build:
    @echo "🔨 Building Docker image..."
    docker build -t {{app_name}} .
    @echo "✅ Docker image built: {{app_name}}"

# Run application in Docker
docker-run:
    @echo "🚀 Running application in Docker..."
    docker run -p 8080:8080 --env-file .env {{app_name}}

# 🧹 Utility Commands

# Clean build artifacts and generated code
clean:
    @echo "🧹 Cleaning build artifacts..."
    rm -rf {{build_dir}}/
    rm -f coverage.out coverage.html
    rm -rf internal/infrastructure/database/query/*.go
    @echo "✅ Cleaned successfully"

# Clean only generated code
clean-gen:
    @echo "🧽 Cleaning generated code..."
    rm -rf internal/infrastructure/database/query/*.go
    rm -f internal/di/wire_gen.go
    @echo "✅ Generated code cleaned"

# Format Go code
fmt:
    @echo "💅 Formatting Go code..."
    go fmt ./...
    @echo "✅ Code formatted"

# Run linter
lint:
    @echo "🔍 Running linter..."
    golangci-lint run
    @echo "✅ Linting completed"

# Download and tidy dependencies
deps:
    @echo "📦 Managing dependencies..."
    go mod download
    go mod tidy
    @echo "✅ Dependencies updated"

# Install required development tools
install-tools:
    @echo "🛠️  Installing development tools..."
    go install github.com/google/wire/cmd/wire@latest
    @echo "💡 Consider installing additional tools:"
    @echo "  - golangci-lint: https://golangci-lint.run/usage/install/"
    @echo "  - air (hot reload): go install github.com/cosmtrek/air@latest"
    @echo "  - entr (file watching): brew install entr"
    @echo "✅ Core tools installed"

# 📚 Documentation Commands

# Generate and serve documentation
docs:
    @echo "📚 Generating documentation..."
    @echo "🌐 API documentation available in docs/"
    @echo "💡 Run 'godoc -http=:6060' for Go documentation server"
    @echo "🔗 Architecture docs:"
    @echo "  - docs/large-scale-architecture.md"
    @echo "  - docs/large-scale-adapter-structure.md"
    @echo "  - docs/gorm-gen-integration.md"

# 🔧 Advanced Development Commands

# Run with hot reload using Air (requires air to be installed)
dev-hot:
    @echo "🔥 Starting development server with hot reload..."
    @echo "💡 Install air: go install github.com/cosmtrek/air@latest"
    air

# Run linter with auto-fix
lint-fix:
    @echo "🔧 Running linter with auto-fix..."
    golangci-lint run --fix

# Security audit
security:
    @echo "🔒 Running security audit..."
    go list -json -deps ./... | nancy sleuth
    @echo "💡 Install nancy: go install github.com/sonatypeoss/nancy@latest"

# Benchmark tests
bench:
    @echo "⚡ Running benchmarks..."
    go test -bench=. -benchmem ./...

# Check for outdated dependencies
deps-check:
    @echo "📊 Checking for outdated dependencies..."
    go list -u -m all

# Initialize new environment file
init-env:
    @echo "📝 Initializing environment file..."
    #!/usr/bin/env bash
    if [ ! -f .env ]; then
        cp .env.example .env
        echo "✅ .env file created from .env.example"
        echo "📝 Please update .env file with your configuration"
    else
        echo "⚠️  .env file already exists"
    fi

# Full reset (clean + install + setup)
reset: clean deps install-tools init-env dev-setup
    @echo "🔄 Full environment reset completed!"

# Show project status
status:
    @echo "📋 Project Status"
    @echo "================"
    @echo "📦 Go version: $(go version)"
    @echo "🏗️  Build directory: {{build_dir}}"
    @echo "🎯 Main file: {{main_path}}"
    @echo "📁 Current directory: $(pwd)"
    @echo ""
    @echo "🔧 Tools status:"
    @which wire > /dev/null && echo "✅ Wire installed" || echo "❌ Wire not installed"
    @which golangci-lint > /dev/null && echo "✅ golangci-lint installed" || echo "❌ golangci-lint not installed"
    @which air > /dev/null && echo "✅ Air installed" || echo "❌ Air not installed (optional)"
    @which docker > /dev/null && echo "✅ Docker installed" || echo "❌ Docker not installed"
    @echo ""
    @echo "📊 Dependencies:"
    @go list -m all | wc -l | xargs echo "Total modules:" 