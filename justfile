# Ghost build and test commands

# Default recipe - show available commands
default:
    @just --list --unsorted

# Build the binary
build:
    go build -o bin/ghost

# Build for specific OS/arch
build-cross os arch:
    GOOS={{os}} GOARCH={{arch}} go build -o bin/ghost-{{os}}-{{arch}}

# Run all tests with verbose output
test:
    go test -v ./...

# Run all tests with short output
test-short:
    go test ./...

# Run tests with coverage report
test-coverage:
    go test -v -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "Coverage report generated: coverage.html"

# Run tests with coverage and open in browser
test-coverage-open: test-coverage
    @echo "Opening coverage report in browser..."
    @open coverage.html 2>/dev/null || xdg-open coverage.html 2>/dev/null || echo "Please open coverage.html manually"

# Run only runner package tests
test-runner:
    go test -v ./internal/runner

# Run only output package tests
test-output:
    go test -v ./internal/output

# Run only cmd package tests  
test-cmd:
    go test -v ./cmd

# Run benchmarks
bench:
    go test -bench=. -benchmem ./...

# Run benchmarks for runner package
bench-runner:
    go test -bench=. -benchmem ./internal/runner

# Clean build artifacts and test files
clean:
    rm -rf bin/
    rm -f coverage.out coverage.html
    rm -rf /tmp/ghost-test-*
    @echo "Cleaned build artifacts and test files"

# Install ghost globally
install: build
    go install
    @echo "Ghost installed to $(go env GOPATH)/bin"

# Build and run ghost with arguments
run *args: build
    ./bin/ghost {{args}}

# Run a test command with ghost
test-run: build
    @echo "Testing ghost with echo command..."
    echo "test input" > /tmp/test_input.txt
    ./bin/ghost run -i /tmp/test_input.txt -o /tmp/test_output.txt -e /tmp/test_error.txt -- echo "Hello from Ghost"
    @echo "Output:"
    @cat /tmp/test_output.txt
    @rm -f /tmp/test_input.txt /tmp/test_output.txt /tmp/test_error.txt

# Run integration tests with various commands
test-integration: build
    @echo "Running integration tests..."
    @bash -c 'set -e; \
        echo "Test 1: Basic echo"; \
        echo "" > /tmp/input.txt; \
        ./bin/ghost run -i /tmp/input.txt -o /tmp/out.txt -e /tmp/err.txt -- echo "test" && \
        echo "test" | cmp -s /tmp/out.txt - && echo "✓ Test 1 passed" || echo "✗ Test 1 failed"; \
        \
        echo "Test 2: Exit code capture"; \
        ./bin/ghost run -i /tmp/input.txt -o /tmp/out.txt -e /tmp/err.txt -- false | jq -e ".exit_code == 1" > /dev/null && \
        echo "✓ Test 2 passed" || echo "✗ Test 2 failed"; \
        \
        echo "Test 3: Score on success"; \
        ./bin/ghost run -i /tmp/input.txt -o /tmp/out.txt -e /tmp/err.txt --score 100 -- true | jq -e ".score == 100" > /dev/null && \
        echo "✓ Test 3 passed" || echo "✗ Test 3 failed"; \
        \
        echo "Test 4: Score on failure"; \
        ./bin/ghost run -i /tmp/input.txt -o /tmp/out.txt -e /tmp/err.txt --score 100 -- false | jq -e ".score == 0" > /dev/null && \
        echo "✓ Test 4 passed" || echo "✗ Test 4 failed"; \
        \
        rm -f /tmp/input.txt /tmp/out.txt /tmp/err.txt'

# Run go fmt on all files
fmt:
    go fmt ./...
    @echo "Code formatted"

# Run go vet
vet:
    go vet ./...
    @echo "Vet completed"

# Run staticcheck if available
lint:
    @which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not found, skipping..."
    go vet ./...

# Run go mod tidy
tidy:
    go mod tidy
    @echo "Dependencies tidied"

# Check code (fmt, vet, test)
check: fmt vet test
    @echo "All checks passed!"

# Watch for changes and rebuild (requires entr)
watch:
    @which entr > /dev/null 2>&1 || (echo "entr not found. Install with: apt-get install entr (debian) or brew install entr (mac)" && exit 1)
    find . -name "*.go" | entr -r just build

# Watch and run tests on change (requires entr)
watch-test:
    @which entr > /dev/null 2>&1 || (echo "entr not found. Install with: apt-get install entr (debian) or brew install entr (mac)" && exit 1)
    find . -name "*.go" | entr -c just test-short

# Generate and serve documentation
docs:
    @echo "Starting godoc server on http://localhost:6060"
    @echo "Navigate to http://localhost:6060/pkg/github.com/zinc-sig/ghost/"
    godoc -http=:6060

# Show current version from git
version:
    @git describe --tags --always --dirty 2>/dev/null || echo "dev"

# Create a new release tag
release version:
    git tag -a v{{version}} -m "Release v{{version}}"
    @echo "Created tag v{{version}}"
    @echo "Push with: git push origin v{{version}}"

# Run security scan with gosec (if available)
security:
    @which gosec > /dev/null 2>&1 && gosec ./... || echo "gosec not found, skipping security scan..."

# Full CI pipeline
ci: clean check security test-coverage test-integration
    @echo "CI pipeline completed successfully!"

# Development setup
setup:
    go mod download
    @echo "Installing development tools..."
    go install honnef.co/go/tools/cmd/staticcheck@latest || true
    go install github.com/securego/gosec/v2/cmd/gosec@latest || true
    @echo "Development environment ready!"

# Quick test - run fast tests only
test-quick:
    go test -short ./...

# Test with race detection
test-race:
    go test -race ./...

# Profile CPU usage during tests
profile-cpu:
    go test -cpuprofile=cpu.prof -bench=. ./internal/runner
    go tool pprof cpu.prof

# Profile memory usage during tests
profile-mem:
    go test -memprofile=mem.prof -bench=. ./internal/runner
    go tool pprof mem.prof