CURRENT_DIR := $(shell pwd)
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_M),x86_64)
	ARCH := amd64
else ifeq ($(UNAME_M),amd64)
	ARCH := amd64
else ifeq ($(UNAME_M),arm64)
	ARCH := arm64
else ifeq ($(UNAME_M),aarch64)
	ARCH := arm64
endif

ifeq ($(UNAME_S),Darwin)
	PLATFORM := darwin
	CGO_LDFLAGS := $(CURRENT_DIR)/lib/darwin_$(ARCH)/liblancedb_go.a -framework Security -framework CoreFoundation
else ifeq ($(UNAME_S),Linux)
	PLATFORM := linux
	CGO_LDFLAGS := $(CURRENT_DIR)/lib/linux_$(ARCH)/liblancedb_go.a -lm -ldl -lpthread
endif

LANCEDB_MOD := $(shell go env GOMODCACHE)/github.com/lancedb/lancedb-go@v0.1.2
CGO_CFLAGS := -I$(LANCEDB_MOD)/include

export CGO_CFLAGS
export CGO_LDFLAGS

.PHONY: build test tidy vet setup-lancedb clean

build: ## Build the mctl binary
	go build -o mctl .

test: ## Run all tests
	go test ./...

tidy: ## Tidy Go modules
	go mod tidy

vet: ## Run go vet
	go vet ./...

setup-lancedb: ## Download LanceDB native libraries for current platform
	bash $(LANCEDB_MOD)/scripts/download-artifacts.sh v0.1.2

clean: ## Remove build artifacts
	rm -f mctl

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'
