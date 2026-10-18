.PHONY: run docker-up docker-down docker-logs

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

# Note: Database migrations and seeding are managed authoritatively by the SkillVerse-SQL_Migration repository:
#   cd ../SkillVerse-SQL_Migration
#   make migrate-up
#   make seed
