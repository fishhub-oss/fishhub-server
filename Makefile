.PHONY: run build dev influx-setup

-include .env
export

INFLUX_TOKEN_FILE := $(CURDIR)/.influxdb-admin-token.json

build:
	go build -o bin/server ./...

run:
	go run ./...

dev:
	echo '{"token":"$(INFLUXDB3_TOKEN)","name":"admin"}' > $(INFLUX_TOKEN_FILE)
	INFLUX_TOKEN_FILE=$(INFLUX_TOKEN_FILE) docker compose up -d
	until docker compose exec postgres pg_isready -U fishhub; do sleep 1; done
	until curl -sf -H "Authorization: Bearer $(INFLUXDB3_TOKEN)" $(INFLUXDB3_HOST)/health > /dev/null; do sleep 1; done
	go run ./...

influx-setup:
	docker compose exec influxdb influxdb3 create database \
	  --token $(INFLUXDB3_TOKEN) $(INFLUXDB3_DATABASE)
