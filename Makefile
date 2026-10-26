.PHONY: build web test check dev clean

build: web
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -tags embed -ldflags='-s -w' -o bin/lnmp-gopanel ./cmd/panel
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
