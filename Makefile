.PHONY: build install test lint fmt tidy check web clean

BINARY  := whetstone
PKG     := ./cmd/whetstone
DIST    := dist
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.buildVersion=$(VERSION)

build:
	@mkdir -p $(DIST)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(PKG)

install:
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

# -race by default: the TUI runs provider calls on goroutines while the model is
# mutated on the event loop, which is exactly where a race would hide.
test:
	go test -race ./...

lint: fmt
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 \
		&& staticcheck ./... \
		|| echo "staticcheck not installed; skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"

fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; \
		echo "run: gofmt -w ."; exit 1; \
	fi

tidy:
	go mod tidy

# Verify the key, endpoint, and model before spending a session on them.
check: build
	./$(DIST)/$(BINARY) -check

# Run the browser UI.
web: build
	./$(DIST)/$(BINARY) -web

clean:
	rm -rf $(DIST) coverage.out
