.PHONY: build web test check dev clean test-privileged test-browser test-security

VERSION ?= dev-$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_LDFLAGS := -s -w -X github.com/azpanel/lnmp-gopanel/internal/buildinfo.Version=$(VERSION) -X github.com/azpanel/lnmp-gopanel/internal/buildinfo.BuiltAt=$(BUILD_TIME)

build: web
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -tags embed -ldflags='$(BUILD_LDFLAGS)' -o bin/lnmp-gopanel ./cmd/panel
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/lnmp-gopanel-control ./cmd/control

web:
	cd web && npm ci && npm run build

test:
	go test -race ./...
	cd web && npm ci && npm test

check:
	go vet ./...
	cd web && npm run build

dev:
	go run ./cmd/panel -config ./config.yaml -services ./services.yaml

clean:
	rm -rf bin web/dist

test-privileged:
	python3 scripts/test-required.py

test-browser: build
	cd web && npm run test:e2e && npm run test:e2e:account && npm run test:e2e:login-logs

test-security:
	go mod verify
	GOMEMLIMIT=2GiB GOGC=50 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	cd web && npm audit
