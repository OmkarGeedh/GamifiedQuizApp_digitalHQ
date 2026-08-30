.PHONY: run docker-up docker-down docker-logs migrate migrate-down migrate-reset migrate-seed migrate-status local-migrate local-seed local-status

# Application Commands
run:
	go run .

# Docker Compose Commands
docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# Database Migration Commands (Docker Environment)
migrate:
	docker compose exec app go run ./cmd/migrate -action=up

migrate-down:
	docker compose exec app go run ./cmd/migrate -action=down

migrate-reset:
	docker compose exec app go run ./cmd/migrate -action=reset

migrate-seed:
	docker compose exec app go run ./cmd/migrate -action=seed

migrate-status:
	docker compose exec app go run ./cmd/migrate -action=status

# Local Host Migration Commands (Direct Host Execution)
local-migrate:
	go run ./cmd/migrate -action=up

local-seed:
	go run ./cmd/migrate -action=seed

local-status:
	go run ./cmd/migrate -action=status
