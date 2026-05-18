# services/daemon/Makefile
#
# Binary naming: vaultmtg-daemon-<os>-<arch>[.exe]
# During the ADR-022 Phase 2 dual-naming window, build-all-dual also produces
# mtga-companion-daemon-* copies so in-field daemons that check the legacy
# artifact name continue to receive updates.  Phase 6 will drop the legacy names.
#
# Must match the artifact names defined in .github/workflows/daemon-release.yml.

.PHONY: build build-all build-all-dual test lint fmt clean

BINARY_PREFIX        := vaultmtg-daemon
BINARY_PREFIX_LEGACY := mtga-companion-daemon
BIN_DIR              := bin

# VERSION: strip the "daemon/" prefix from tags like "daemon/v0.3.0" -> "0.3.0".
# Falls back to "dev" if no matching tag is found.
RAW_VERSION   := $(shell git describe --tags --match 'daemon/v*' --abbrev=0 2>/dev/null)
VERSION       := $(if $(RAW_VERSION),$(patsubst daemon/v%,%,$(RAW_VERSION)),dev)

LDFLAGS := -s -w -X main.Version=$(VERSION)

# Default: build for the local machine.
build:
	go build -v -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_PREFIX) ./cmd/daemon

# Cross-compile all release targets (new names only).
build-all: build-windows-amd64 build-darwin-arm64 build-darwin-amd64

# Cross-compile all targets and produce legacy-named copies (ADR-022 Phase 2 dual-naming).
# Remove this target in Phase 6 once all in-field daemons have migrated.
build-all-dual: build-all
	cp $(BIN_DIR)/$(BINARY_PREFIX)-windows-amd64.exe \
	   $(BIN_DIR)/$(BINARY_PREFIX_LEGACY)-windows-amd64.exe
	cp $(BIN_DIR)/$(BINARY_PREFIX)-darwin-arm64 \
	   $(BIN_DIR)/$(BINARY_PREFIX_LEGACY)-darwin-arm64
	cp $(BIN_DIR)/$(BINARY_PREFIX)-darwin-amd64 \
	   $(BIN_DIR)/$(BINARY_PREFIX_LEGACY)-darwin-amd64

build-windows-amd64:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
	  go build -v -ldflags="$(LDFLAGS)" \
	  -o $(BIN_DIR)/$(BINARY_PREFIX)-windows-amd64.exe ./cmd/daemon

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
	  go build -v -ldflags="$(LDFLAGS)" \
	  -o $(BIN_DIR)/$(BINARY_PREFIX)-darwin-arm64 ./cmd/daemon

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 \
	  go build -v -ldflags="$(LDFLAGS)" \
	  -o $(BIN_DIR)/$(BINARY_PREFIX)-darwin-amd64 ./cmd/daemon

test:
	go test -race ./...

lint:
	gofumpt -l .
	go vet ./...

fmt:
	gofumpt -w ./...

clean:
	rm -rf $(BIN_DIR)
