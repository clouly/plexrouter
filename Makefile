VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build web server run dev-web test lint docker clean

## build: build the web console and the server binary into bin/
build: web server

web:
	cd web && npm ci && npm run build

server:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/plexrouter ./cmd/plexrouter

## run: run the server from source (serves the last built console)
run:
	go run ./cmd/plexrouter

## dev-web: start the Vite dev server (proxies /api and /v1 to :8080)
dev-web:
	cd web && npm run dev

test:
	go test -race ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l cmd internal web/*.go)"

docker:
	docker build --build-arg VERSION=$(VERSION) -t plexrouter:$(VERSION) .

clean:
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
