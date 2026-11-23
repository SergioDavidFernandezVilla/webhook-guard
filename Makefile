COMPOSE ?= docker compose
SERVICE ?= app

.PHONY: up down logs ps build migrate load naive test tidy

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

test:
	go test ./...

tidy:
	go mod tidy
