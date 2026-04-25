.PHONY: run build dev influx-setup

-include .env
export

DEVICE_JWT_PRIVATE_KEY = $(shell awk '{printf "%s\\n", $$0}' secrets/device_jwt_private_key.pem 2>/dev/null)

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
	until curl -sf http://localhost:3000/api/health > /dev/null; do sleep 1; done
	@echo "\n📡 Server IP addresses:"
	@ipconfig getifaddr en0 2>/dev/null && echo "  (Wi-Fi)" || true
	@ipconfig getifaddr en1 2>/dev/null && echo "  (Ethernet)" || true
	@echo ""
	go run ./... || true
	docker compose down

influx-setup:
	docker compose exec influxdb influxdb3 create database \
	  --token $(INFLUXDB3_TOKEN) $(INFLUXDB3_DATABASE)
