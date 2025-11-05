# 查找所有包含 go.mod 的目录
GO_MODULE_DIRS := $(shell find . -type f -name "go.mod" -exec dirname {} \;)

.PHONY: all build test tidy

all: tidy build test

# 执行 go mod tidy
tidy:
	@for dir in $(GO_MODULE_DIRS); do \
		echo "[TIDY] $$dir"; \
		(cd $$dir && go mod tidy); \
	done

# 执行 go build
build:
	@for dir in $(GO_MODULE_DIRS); do \
		echo "[BUILD] $$dir"; \
		(cd $$dir && go build ./...); \
	done

# 执行 go test
test:
	@for dir in $(GO_MODULE_DIRS); do \
		echo "[TEST] $$dir"; \
		(cd $$dir && go test ./...); \
	done

