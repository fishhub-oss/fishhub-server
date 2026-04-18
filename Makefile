.PHONY: run build dev

DATABASE_URL ?= postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable

build:
	go build -o bin/server ./...

run:
	DATABASE_URL=$(DATABASE_URL) go run ./...

dev:
	docker compose up -d
	until docker compose exec postgres pg_isready -U fishhub; do sleep 1; done
	DATABASE_URL=$(DATABASE_URL) go run ./...
