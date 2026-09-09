GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

.PHONY: build backend test check integration up down
build:
	npm run build
	$(MAKE) backend

backend:
	mkdir -p dist
	CGO_ENABLED=1 go build -trimpath -o dist/gpx_local_duckdb_$(GOOS)_$(GOARCH) ./pkg

test:
	go test ./pkg/... ./cmd/...
	npm test

check:
	go vet ./pkg/... ./cmd/...
	npm run typecheck
	npm run lint

integration:
	python3 scripts/test-ducklake.py

up:
	docker compose up --build -d --wait

down:
	docker compose down
