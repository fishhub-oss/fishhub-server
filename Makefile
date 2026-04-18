.PHONY: run build dev influx-setup

DATABASE_URL      ?= postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable
INFLUXDB3_HOST     ?= http://localhost:8181
INFLUXDB3_TOKEN    ?= $(shell cat secrets/influxdb-admin-token.json 2>/dev/null | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
INFLUXDB3_DATABASE ?= readings

build:
	go build -o bin/server ./...

run:
	DATABASE_URL=$(DATABASE_URL) go run ./...

dev:
	docker compose up -d
	until docker compose exec postgres pg_isready -U fishhub; do sleep 1; done
	DATABASE_URL=$(DATABASE_URL) \
	INFLUXDB3_HOST=$(INFLUXDB3_HOST) \
	INFLUXDB3_TOKEN=$(INFLUXDB3_TOKEN) \
	INFLUXDB3_DATABASE=$(INFLUXDB3_DATABASE) \
	go run ./...

influx-setup:
	docker compose exec influxdb influxdb3 create database \
	  --token $(INFLUXDB3_TOKEN) $(INFLUXDB3_DATABASE)
