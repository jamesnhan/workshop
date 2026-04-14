.PHONY: build dev clean frontend backend install deploy-local test test-unit test-integration test-race test-cover test-frontend install-hooks

PREFIX ?= $(HOME)/.local
COVERAGE_DIR := coverage

build: frontend backend
	@echo "Built: bin/workshop"

frontend:
	cd frontend && npm ci && npm run build

backend:
	go build -o bin/workshop .

install: build
	install -d $(PREFIX)/bin
	install -m 755 bin/workshop $(PREFIX)/bin/workshop
	@echo "Installed: $(PREFIX)/bin/workshop"

# --- Deploy ---

# Local agent: build, install, restart service.
# Linux uses systemd, macOS uses launchctl.
deploy-local: install
ifeq ($(shell uname),Darwin)
	launchctl kickstart -k gui/$$(id -u)/com.jamesnhan.workshop 2>/dev/null || true
	@echo "Local agent deployed. (launchctl restart attempted — start manually if no launchd plist installed)"
else
	systemctl --user restart workshop.service
	@echo "Local agent deployed and restarted."
endif

dev:
	@echo "Start Go backend:  go run ."
	@echo "Start Vite dev:    cd frontend && npm run dev"
	@echo "Run both in separate terminals, or use tmux."

clean:
	rm -rf bin/ frontend/dist frontend/node_modules $(COVERAGE_DIR)

# --- Tests ---

# Default test target: fast unit tests only (no -race, no integration).
test: test-unit

# Unit tests: everything not tagged `integration`.
test-unit:
	go test ./ ./internal/...

# Integration tests: anything tagged `integration`. Slower, may touch tmux,
# the filesystem, or an in-process HTTP server.
test-integration:
	go test -tags=integration ./ ./internal/...

# Race detector over the full tree. Used in CI.
test-race:
	go test -race ./ ./internal/...

# Coverage report. Writes to coverage/backend.out and generates HTML.
test-cover:
	@mkdir -p $(COVERAGE_DIR)
	go test -coverprofile=$(COVERAGE_DIR)/backend.out ./ ./internal/...
	go tool cover -html=$(COVERAGE_DIR)/backend.out -o $(COVERAGE_DIR)/backend.html
	@echo "Coverage report: $(COVERAGE_DIR)/backend.html"

# Frontend tests (delegates to Vitest — wired up in #477).
test-frontend:
	cd frontend && npm test

# Install the checked-in git hooks (pre-push runs fast tests). Run once
# after cloning. Use `git push --no-verify` to bypass in emergencies.
install-hooks:
	git config core.hooksPath .githooks
	@echo "Hooks installed. Pre-push will now run 'make test-unit' and 'make test-frontend'."
