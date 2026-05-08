.PHONY: run build dev worker influx-setup emqx-setup

-include .env
export

DEVICE_JWT_PRIVATE_KEY = $(shell awk '{printf "%s\\n", $$0}' secrets/device_jwt_private_key.pem 2>/dev/null)

INFLUX_TOKEN_FILE := $(CURDIR)/.influxdb-admin-token.json

build:
	go build -o bin/server .
	go build -o bin/worker ./cmd/worker

run:
	go run .

worker:
	go run ./cmd/worker

dev:
	echo '{"token":"$(INFLUXDB3_TOKEN)","name":"admin"}' > $(INFLUX_TOKEN_FILE)
	INFLUX_TOKEN_FILE=$(INFLUX_TOKEN_FILE) docker compose up -d
	until docker compose exec postgres pg_isready -U fishhub; do sleep 1; done
	until curl -sf -H "Authorization: Bearer $(INFLUXDB3_TOKEN)" $(INFLUXDB3_HOST)/health > /dev/null; do sleep 1; done
	until curl -sf http://localhost:3000/api/health > /dev/null; do sleep 1; done
	until curl -sf http://localhost:18083/api/v5/status > /dev/null; do sleep 1; done
	@echo "\n📡 Server IP addresses:"
	@ipconfig getifaddr en0 2>/dev/null && echo "  (Wi-Fi)" || true
	@ipconfig getifaddr en1 2>/dev/null && echo "  (Ethernet)" || true
	@echo ""
	go run . || true
	docker compose down

influx-setup:
	docker compose exec influxdb influxdb3 create database \
	  --token $(INFLUXDB3_TOKEN) $(INFLUXDB3_DATABASE)

emqx-setup:
	until curl -sf http://localhost:18083/api/v5/status > /dev/null; do sleep 1; done
	@echo "→ Creating password_based:built_in_database authentication backend..."
	curl -sf -u "$(EMQX_API_KEY):$(EMQX_API_SECRET)" \
	  -X POST http://localhost:18083/api/v5/authentication \
	  -H "Content-Type: application/json" \
	  -d '{"backend":"built_in_database","mechanism":"password_based","password_hash_algorithm":{"name":"bcrypt"},"user_id_type":"username"}' \
	  -o /dev/null || true
	@echo "→ Creating server MQTT credential ($(EMQX_SERVER_USERNAME))..."
	curl -sf -u "$(EMQX_API_KEY):$(EMQX_API_SECRET)" \
	  -X POST http://localhost:18083/api/v5/authentication/$(EMQX_AUTH_ID)/users \
	  -H "Content-Type: application/json" \
	  -d '{"user_id":"$(EMQX_SERVER_USERNAME)","password":"$(EMQX_SERVER_PASSWORD)"}' \
	  -o /dev/null || true
	@echo "✓ EMQX setup complete"
