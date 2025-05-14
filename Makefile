.PHONY: db-up db-down db-restart db-logs db-shell db-migrate

# Start the PostgreSQL database
db-up:
	docker-compose up -d postgres

# Stop the PostgreSQL database
db-down:
	docker-compose down

# Restart the PostgreSQL database
db-restart:
	docker-compose restart postgres

# View database logs
db-logs:
	docker-compose logs -f postgres

# Open a PostgreSQL shell
db-shell:
	docker exec -it token_transfer_db psql -U postgres -d token_transfer

# Clean database volumes (WARNING: This will delete all data)
db-clean:
	docker-compose down -v

# Check database health
db-health:
	docker exec token_transfer_db pg_isready -U postgres