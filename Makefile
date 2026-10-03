# Nodera
#
# Go services read cmd/<binary>/.conf/dev.toml (gitignored; copy the .example).
# docker-compose carries infrastructure only.

LOG_DIR := logs
PID_DIR := .run

# Tools built with the module's own Go version live in .local/bin (gitignored);
# `make tools` installs them. A globally installed tool built with an older Go
# refuses to parse go 1.27 sources.
export PATH := $(CURDIR)/.local/bin:$(PATH)
# GNU make 3.81 (macOS) execs a one-command recipe without a shell and ignores
# the exported PATH for it, so the tools are also named by absolute path.
LOCAL_BIN := $(CURDIR)/.local/bin

tools:
	GOBIN=$(CURDIR)/.local/bin go install github.com/swaggo/swag/cmd/swag@v1.16.6
	GOBIN=$(CURDIR)/.local/bin go install go.uber.org/mock/mockgen@v0.6.0
	GOBIN=$(CURDIR)/.local/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	GOBIN=$(CURDIR)/.local/bin go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.0

$(LOG_DIR) $(PID_DIR):
	@mkdir -p $@

# =============================================================================
# Infrastructure
# =============================================================================

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-ps:
	docker compose ps

# Run once after the first `docker-up`: turns the single mongo into a replica set.
mongo-init:
	docker compose exec mongo mongosh --quiet --eval 'try { rs.status().ok } catch (e) { rs.initiate({_id: "rs0", members: [{_id: 0, host: "localhost:27017"}]}) }'

# =============================================================================
# Customer
# =============================================================================

server-customer:
	$(MAKE) -s swagger-customer
	go run ./cmd/customer server --env=dev

outbox-customer:
	go run ./cmd/customer outbox --env=dev

migrate-customer:
	go run ./cmd/customer migrate --env=dev

rollback-customer:
	go run ./cmd/customer rollback --env=dev

seed-customer:
	go run ./cmd/customer seed --env=dev

seed-rollback-customer:
	go run ./cmd/customer seed-rollback --env=dev

# DESTRUCTIVE: drop and recreate the customer database, then migrate and seed.
reset-db-customer:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_customer WITH (FORCE);' -c 'CREATE DATABASE nodera_customer;'
	$(MAKE) -s migrate-customer
	$(MAKE) -s seed-customer

# Scoped to the customer dirs: with two `dto` packages in the repo, swag cannot
# resolve type names when it parses the whole tree.
swagger-customer:
	$(LOCAL_BIN)/swag init -q -d ./cmd/customer,./contexts/customer -g server.go -o contexts/customer/docs --parseDependency --parseInternal

# =============================================================================
# Droplet
# =============================================================================

server-droplet:
	$(MAKE) -s swagger-droplet
	go run ./cmd/droplet server --env=dev

worker-droplet:
	go run ./cmd/droplet worker --env=dev

migrate-droplet:
	go run ./cmd/droplet migrate --env=dev

rollback-droplet:
	go run ./cmd/droplet rollback --env=dev

seed-droplet:
	go run ./cmd/droplet seed --env=dev

seed-rollback-droplet:
	go run ./cmd/droplet seed-rollback --env=dev

# DESTRUCTIVE: drop and recreate the droplet database, then migrate and seed.
reset-db-droplet:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_droplet WITH (FORCE);' -c 'CREATE DATABASE nodera_droplet;'
	$(MAKE) -s migrate-droplet
	$(MAKE) -s seed-droplet

# Scoped to the droplet dirs: with two `dto` packages in the repo, swag cannot
# resolve type names when it parses the whole tree.
swagger-droplet:
	$(LOCAL_BIN)/swag init -q -d ./cmd/droplet,./contexts/droplet -g server.go -o contexts/droplet/docs --parseDependency --parseInternal

# =============================================================================
# Billing
# =============================================================================

server-billing:
	$(MAKE) -s swagger-billing
	go run ./cmd/billing server --env=dev

worker-billing:
	go run ./cmd/billing worker --env=dev

outbox-droplet:
	go run ./cmd/droplet outbox --env=dev

outbox-billing:
	go run ./cmd/billing outbox --env=dev

migrate-billing:
	go run ./cmd/billing migrate --env=dev

rollback-billing:
	go run ./cmd/billing rollback --env=dev

seed-billing:
	go run ./cmd/billing seed --env=dev

seed-rollback-billing:
	go run ./cmd/billing seed-rollback --env=dev

# DESTRUCTIVE: drop and recreate the billing database, then migrate and seed.
reset-db-billing:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_billing WITH (FORCE);' -c 'CREATE DATABASE nodera_billing;'
	$(MAKE) -s migrate-billing
	$(MAKE) -s seed-billing

# Scoped to the billing dirs: with several `dto` packages in the repo, swag
# cannot resolve type names when it parses the whole tree.
swagger-billing:
	$(LOCAL_BIN)/swag init -q -d ./cmd/billing,./contexts/billing -g server.go -o contexts/billing/docs --parseDependency --parseInternal

# =============================================================================
# Customer gateway
# =============================================================================

server-customer-gateway:
	go run ./cmd/customer-gateway server --env=dev

# =============================================================================
# Notification
# =============================================================================

server-notification:
	$(MAKE) -s swagger-notification
	go run ./cmd/notification server --env=dev

worker-notification:
	go run ./cmd/notification worker --env=dev

migrate-notification:
	go run ./cmd/notification migrate --env=dev

# Dev only: sends a sample Notification to a Team through RabbitMQ, as its real
# event would, so the worker, the socket and the web app all run. TEAM is the id
# on the web app's Team page; TYPE is a Notification type about a Team, or all.
#   make sample-notification TEAM=<team id>
#   make sample-notification TEAM=<team id> TYPE=payment_failed
#   make sample-notification TEAM=<team id> TYPE=all
sample-notification:
	@test -n "$(TEAM)" || (echo "usage: make sample-notification TEAM=<team id> [TYPE=droplet_active|...|all]" && exit 1)
	go run ./cmd/notification sample --env=dev --team=$(TEAM) --type=$(or $(TYPE),droplet_active)

rollback-notification:
	go run ./cmd/notification rollback --env=dev

# Creates the database on a postgres volume made before it was added to
# scripts/init-db.sql; harmless when it exists.
create-db-notification:
	docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'nodera_notification'" | grep -q 1 || \
		docker compose exec -T postgres psql -U postgres -c 'CREATE DATABASE nodera_notification;'

# DESTRUCTIVE: drop and recreate the notification database, then migrate.
reset-db-notification:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_notification WITH (FORCE);' -c 'CREATE DATABASE nodera_notification;'
	$(MAKE) -s migrate-notification

# Scoped to the notification dirs: with several `dto` packages in the repo,
# swag cannot resolve type names when it parses the whole tree.
swagger-notification:
	$(LOCAL_BIN)/swag init -q -d ./cmd/notification,./contexts/notification -g server.go -o contexts/notification/docs --parseDependency --parseInternal

# =============================================================================
# Realtime
# =============================================================================

server-realtime:
	go run ./cmd/realtime server --env=dev

# =============================================================================
# Lifecycle: every dev daemon in the background with logs under logs/
# =============================================================================

start: | $(LOG_DIR) $(PID_DIR)
	@$(MAKE) -s swagger-customer
	@$(MAKE) -s swagger-droplet
	@$(MAKE) -s swagger-billing
	@$(MAKE) -s swagger-notification
	@touch $(LOG_DIR)/customer-server.log $(LOG_DIR)/customer-outbox.log $(LOG_DIR)/customer-gateway.log $(LOG_DIR)/notification-worker.log $(LOG_DIR)/droplet-server.log $(LOG_DIR)/droplet-worker.log
	@( go run ./cmd/customer server --env=dev > $(LOG_DIR)/customer-server.log 2>&1 ) & echo $$! > $(PID_DIR)/customer-server.pid
	@( go run ./cmd/customer outbox --env=dev > $(LOG_DIR)/customer-outbox.log 2>&1 ) & echo $$! > $(PID_DIR)/customer-outbox.pid
	@( go run ./cmd/customer-gateway server --env=dev > $(LOG_DIR)/customer-gateway.log 2>&1 ) & echo $$! > $(PID_DIR)/customer-gateway.pid
	@( go run ./cmd/notification worker --env=dev > $(LOG_DIR)/notification-worker.log 2>&1 ) & echo $$! > $(PID_DIR)/notification-worker.pid
	@touch $(LOG_DIR)/notification-server.log $(LOG_DIR)/realtime-server.log
	@( go run ./cmd/notification server --env=dev > $(LOG_DIR)/notification-server.log 2>&1 ) & echo $$! > $(PID_DIR)/notification-server.pid
	@( go run ./cmd/realtime server --env=dev > $(LOG_DIR)/realtime-server.log 2>&1 ) & echo $$! > $(PID_DIR)/realtime-server.pid
	@( go run ./cmd/droplet server --env=dev > $(LOG_DIR)/droplet-server.log 2>&1 ) & echo $$! > $(PID_DIR)/droplet-server.pid
	@( go run ./cmd/droplet worker --env=dev > $(LOG_DIR)/droplet-worker.log 2>&1 ) & echo $$! > $(PID_DIR)/droplet-worker.pid
	@touch $(LOG_DIR)/droplet-outbox.log
	@( go run ./cmd/droplet outbox --env=dev > $(LOG_DIR)/droplet-outbox.log 2>&1 ) & echo $$! > $(PID_DIR)/droplet-outbox.pid
	@touch $(LOG_DIR)/billing-server.log $(LOG_DIR)/billing-worker.log $(LOG_DIR)/billing-outbox.log
	@( go run ./cmd/billing server --env=dev > $(LOG_DIR)/billing-server.log 2>&1 ) & echo $$! > $(PID_DIR)/billing-server.pid
	@( go run ./cmd/billing worker --env=dev > $(LOG_DIR)/billing-worker.log 2>&1 ) & echo $$! > $(PID_DIR)/billing-worker.pid
	@( go run ./cmd/billing outbox --env=dev > $(LOG_DIR)/billing-outbox.log 2>&1 ) & echo $$! > $(PID_DIR)/billing-outbox.pid
	@echo "started. Tailing logs (Ctrl-C to stop)."
	@trap '$(MAKE) -s stop' INT TERM EXIT; tail -f $(LOG_DIR)/*.log

stop:
	@for f in $(PID_DIR)/*.pid; do \
	  [ -f "$$f" ] || continue; \
	  pid=$$(cat "$$f"); \
	  pkill -P $$pid 2>/dev/null || true; \
	  kill $$pid 2>/dev/null || true; \
	  rm -f "$$f"; \
	done; \
	echo "stopped"

logs-tail:
	@tail -f $(LOG_DIR)/*.log

# =============================================================================
# Quality
# =============================================================================

test:
	go test ./...

# Needs Docker and the `migrate` CLI on PATH.
test-all:
	INTEGRATION=true go test ./... -count=1

test-customer-integration:
	INTEGRATION=true go test ./cmd/customer/integration-test/ -count=1 -v

test-gateway-integration:
	INTEGRATION=true go test ./cmd/customer-gateway/ -count=1 -v

test-droplet-integration:
	INTEGRATION=true go test ./cmd/droplet/integration-test/ -count=1 -v

test-billing-integration:
	INTEGRATION=true go test ./cmd/billing/integration-test/ -count=1 -v

test-notification-integration:
	INTEGRATION=true go test ./cmd/notification/integration-test/ -count=1 -v

test-realtime-integration:
	INTEGRATION=true go test ./cmd/realtime/integration-test/ -count=1 -v

lint:
	$(LOCAL_BIN)/golangci-lint run --config .golangci.yml ./...

format:
	gofmt -s -w .

generate:
	go generate ./...
