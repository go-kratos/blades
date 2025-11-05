# Find all directories that contain a go.mod file
GO_MODULE_DIRS := $(shell find . -type f -name "go.mod" -exec dirname {} \;)

.PHONY: all build test tidy

# Default target: run tidy, build, and test
all: tidy build test

# Run 'go mod tidy' in each Go module
tidy:
	@for dir in $(GO_MODULE_DIRS); do \
		echo "[TIDY] $$dir"; \
		(cd $$dir && go mod tidy); \
	done

# Run 'go build' in each Go module
build:
	@for dir in $(GO_MODULE_DIRS); do \
		echo "[BUILD] $$dir"; \
		(cd $$dir && go build ./...); \
	done

# Run 'go test' in each Go module
test:
	@for dir in $(GO_MODULE_DIRS); do \
		echo "[TEST] $$dir"; \
		(cd $$dir && go test ./...); \
	done

