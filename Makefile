GO ?= go
NPM ?= npm

.PHONY: build clean test typecheck verify web-install web-build

web-install:
	cd web && $(NPM) ci

web-build:
	cd web && $(NPM) run build

build: web-build
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/panel ./cmd/panel
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/panelctl ./cmd/panelctl

test:
	$(GO) test -race ./...
	cd web && $(NPM) test

typecheck:
	cd web && $(NPM) run typecheck

verify: test typecheck web-build
	$(GO) vet ./...
	$(GO) mod verify
	cd web && $(NPM) audit --audit-level=high

clean:
	rm -rf bin dist web/dist web/coverage web/playwright-report web/test-results coverage.out
