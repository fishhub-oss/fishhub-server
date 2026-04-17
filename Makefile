.PHONY: run build

build:
	go build -o bin/server ./...

run:
	go run ./...
