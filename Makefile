COMPOSE = docker compose -f deploy/docker-compose.yaml --env-file .env

.PHONY: up down build logs ps restart-backend

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

build:
	$(COMPOSE) build --no-cache

logs:
	$(COMPOSE) logs -f --tail=100

ps:
	$(COMPOSE) ps

restart-backend:
	$(COMPOSE) up -d --force-recreate pbr_backend
