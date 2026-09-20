.DEFAULT_GOAL := help
SERVICE ?=

help: ## Show available commands and usage
	@echo "Usage: make <target> [SERVICE=<name>]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS=":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

up: ## Build images and start all services
	docker compose up --build -d

ps: ## Show service status
	docker compose ps

logs: ## Tail logs of all services, or SERVICE=x for one
	docker compose logs -f $(SERVICE)

health: ## Check liveness/readiness of local dependencies
	@curl -sf http://localhost:8080/health/live > /dev/null && echo "api live: OK"
	@curl -sf http://localhost:8080/health/ready > /dev/null && echo "api ready: OK"
	@docker compose exec -T postgres pg_isready -U postgres
	@curl -sf http://localhost:4566/_ministack/health > /dev/null && echo "ministack: OK"
	@curl -sf http://localhost:8081/realms/wallet/ > /dev/null && echo "keycloak: OK"

test: ## Run unit tests
	go test ./...

test-race: ## Run unit tests with the race detector
	go test -race ./...

test-integration: ## Run integration tests (needs up)
	go test -tags integration ./tests/ -timeout 5m

vet: ## Vet all packages including integration-tagged files
	go vet ./... && go vet -tags integration ./...

migrate-up: ## Apply pending migrations
	go run ./cmd/migrate -command up

migrate-down: ## Revert ALL migrations (asks confirmation)
	@read -p "Revert ALL migrations? [y/N] " c; [ "$$c" = "y" ] && go run ./cmd/migrate -command down-to -args 0

stop: ## Stop services, keep containers and volumes
	docker compose stop

clean: ## Remove containers and network, keep volumes (safe)
	docker compose down

nuke: ## Destroy containers, volumes and local images (asks confirmation)
	@echo "Will destroy containers, network, volumes and locally built images:"
	@docker compose config --images 2>/dev/null | sort -u | sed 's/^/  image: /'
	@docker compose config --volumes 2>/dev/null | sort -u | sed 's/^/  volume: /'
	@read -p 'Type NUKE to confirm: ' c; [ "$$c" = "NUKE" ] && docker compose down -v --rmi local
