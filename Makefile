COMPOSE ?= docker compose
SERVICE ?= app

.PHONY: up down logs ps build migrate load naive dashboard dashboard-build dashboard-dev test tidy

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f $(SERVICE)

ps:
	$(COMPOSE) ps

build:
	$(COMPOSE) build

migrate:
	$(COMPOSE) run --rm migrate

load:
	$(COMPOSE) --profile load run --rm loadgen

naive:
	$(COMPOSE) --profile naive up -d --build app-naive

dashboard:
	$(COMPOSE) --profile dashboard up -d --build dashboard

dashboard-build:
	$(COMPOSE) --profile dashboard build dashboard

dashboard-dev:
	cd web && npm run dev

test:
	go test ./...

tidy:
	go mod tidy
