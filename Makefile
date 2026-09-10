.PHONY: install frontend build run check test-integration db dev

# Development-only connection matching compose.yaml. The executable itself
# requires ACTA_DATABASE_URL explicitly and has no database/password fallback.
DEV_DATABASE_URL ?= postgres://acta2:acta2-development-only@127.0.0.1:5433/acta2?sslmode=disable

install:
	npm --prefix web ci

frontend:
	npm --prefix web run build

build: frontend
	go build -trimpath -o bin/acta2-server ./cmd/acta2
	go build -trimpath -o bin/acta2 ./cmd/acta2-cli
	go build -trimpath -o bin/acta2-backup ./cmd/acta2-backup
	go build -trimpath -o bin/acta2-update ./cmd/acta2-update

db:
	docker compose up -d --wait db

run: build
	ACTA_DATABASE_URL="$${ACTA_DATABASE_URL:-$(DEV_DATABASE_URL)}" ACTA_DEV_ORIGIN="$${ACTA_DEV_ORIGIN:-http://localhost:5173}" ./bin/acta2-server -listen :8081

check: frontend
	npm --prefix web run format:check
	npm --prefix web run check
	npm --prefix web test
	go vet ./...
	go test ./...
	@test -z "$$(gofmt -l cmd internal web/*.go)"

test-integration:
	@test -n "$(ACTA_TEST_DATABASE_URL)" || (echo 'Set ACTA_TEST_DATABASE_URL to a PostgreSQL URL.' >&2; exit 1)
	go test -race -count=1 ./...

dev:
	npm --prefix web run dev
