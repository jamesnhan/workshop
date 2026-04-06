.PHONY: build dev clean frontend backend install

PREFIX ?= $(HOME)/.local

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

dev:
	@echo "Start Go backend:  go run ."
	@echo "Start Vite dev:    cd frontend && npm run dev"
	@echo "Run both in separate terminals, or use tmux."

clean:
	rm -rf bin/ frontend/dist frontend/node_modules
