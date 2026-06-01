.PHONY: run build test vet tidy db-up db-down

run:
	go run ./cmd/acta serve

build:
	go build -o bin/acta ./cmd/acta

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

db-up:
	docker compose up -d db

db-down:
	docker compose down
