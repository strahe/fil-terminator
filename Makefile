.PHONY: build clean install test

# Build parameters
BINARY_NAME = fil-terminator
GIT_COMMIT = $(shell git rev-parse --short HEAD)
LDFLAGS = -X github.com/strahe/fil-terminator/version.CurrentCommit=$(GIT_COMMIT)

# Default target
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/fil-terminator

# Clean build artifacts
clean:
	go clean
	rm -f $(BINARY_NAME)

# Install binary
install: build
	cp $(BINARY_NAME) /usr/local/bin/

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...
