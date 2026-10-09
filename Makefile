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

# Dev only: saves a Payment method no Payment provider holds for a Team, since
# dev has no provider credentials for the web app's card form. The Team's first
# one makes it active, so it can create Droplets and choose Mail Packages; the
# billing outbox relays that. TEAM is the id on the web app's Team page, and the
# Team needs its Billing profile first.
#   make sample-payment-method TEAM=<team id>
sample-payment-method:
	@test -n "$(TEAM)" || (echo "usage: make sample-payment-method TEAM=<team id>" && exit 1)
	go run ./cmd/billing sample-payment-method --env=dev --team=$(TEAM)

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
# Mail
# =============================================================================

server-mail:
	$(MAKE) -s swagger-mail
	go run ./cmd/mail server --env=dev

smtp-mail:
	go run ./cmd/mail smtp --env=dev

dns-mail:
	go run ./cmd/mail dns --env=dev

worker-mail:
	go run ./cmd/mail worker --env=dev

outbox-mail:
	go run ./cmd/mail outbox --env=dev

migrate-mail:
	go run ./cmd/mail migrate --env=dev

rollback-mail:
	go run ./cmd/mail rollback --env=dev

seed-mail:
	go run ./cmd/mail seed --env=dev

seed-rollback-mail:
	go run ./cmd/mail seed-rollback --env=dev

# An operator's decision until the Admin side exists: lifts a Project's Pause.
#   make unpause-mail PROJECT=<project id>
unpause-mail:
	@test -n "$(PROJECT)" || (echo "usage: make unpause-mail PROJECT=<project id>" && exit 1)
	go run ./cmd/mail unpause --env=dev --project=$(PROJECT)

# Dev only: verifies a Sending domain as if the Team had published its DNS
# records. Dev's zone is not public, so the real check cannot pass; the daily
# check unverifies the domain again a day later. SENDING_DOMAIN is the last
# part of the domain page's URL in the web app.
#   make verify-sending-domain SENDING_DOMAIN=<sending domain id>
verify-sending-domain:
	@test -n "$(SENDING_DOMAIN)" || (echo "usage: make verify-sending-domain SENDING_DOMAIN=<sending domain id>" && exit 1)
	go run ./cmd/mail verify-sending-domain --env=dev --sending-domain=$(SENDING_DOMAIN)

# Creates the database on a postgres volume made before it was added to
# scripts/init-db.sql; harmless when it exists.
create-db-mail:
	docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'nodera_mail'" | grep -q 1 || \
		docker compose exec -T postgres psql -U postgres -c 'CREATE DATABASE nodera_mail;'

# DESTRUCTIVE: drop and recreate the mail database, then migrate and seed.
reset-db-mail:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_mail WITH (FORCE);' -c 'CREATE DATABASE nodera_mail;'
	$(MAKE) -s migrate-mail
	$(MAKE) -s seed-mail

# Scoped to the mail dirs: with several `dto` packages in the repo, swag
# cannot resolve type names when it parses the whole tree.
swagger-mail:
	$(LOCAL_BIN)/swag init -q -d ./cmd/mail,./contexts/mail -g server.go -o contexts/mail/docs --parseDependency --parseInternal

# =============================================================================
# Storage
# =============================================================================

server-storage:
	$(MAKE) -s swagger-storage
	go run ./cmd/storage server --env=dev

worker-storage:
	go run ./cmd/storage worker --env=dev

outbox-storage:
	go run ./cmd/storage outbox --env=dev

migrate-storage:
	go run ./cmd/storage migrate --env=dev

rollback-storage:
	go run ./cmd/storage rollback --env=dev

seed-storage:
	go run ./cmd/storage seed --env=dev

seed-rollback-storage:
	go run ./cmd/storage seed-rollback --env=dev

# Operator: stop a Storage after a complaint, or reopen it.
#   make pause-storage STORAGE=<storage id> REASON=abuse_complaint|malware|phishing|legal
#   make unpause-storage STORAGE=<storage id>
pause-storage:
	@test -n "$(STORAGE)" -a -n "$(REASON)" || (echo "usage: make pause-storage STORAGE=<storage id> REASON=<reason>" && exit 1)
	go run ./cmd/storage pause --env=dev --storage=$(STORAGE) --reason=$(REASON)

unpause-storage:
	@test -n "$(STORAGE)" || (echo "usage: make unpause-storage STORAGE=<storage id>" && exit 1)
	go run ./cmd/storage unpause --env=dev --storage=$(STORAGE)

# The database is created by scripts/init-db.sql on a fresh volume; this is
# for a volume that predates it.
create-db-storage:
	docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'nodera_storage'" | grep -q 1 || \
		docker compose exec -T postgres psql -U postgres -c 'CREATE DATABASE nodera_storage;'

# DESTRUCTIVE: drop and recreate the storage database, then migrate and seed.
reset-db-storage:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_storage WITH (FORCE);' -c 'CREATE DATABASE nodera_storage;'
	$(MAKE) -s migrate-storage
	$(MAKE) -s seed-storage

swagger-storage:
	$(LOCAL_BIN)/swag init -q -d ./cmd/storage,./contexts/storage -g server.go -o contexts/storage/docs --parseDependency --parseInternal

# =============================================================================
# Kubernetes
# =============================================================================

server-kubernetes:
	$(MAKE) -s swagger-kubernetes
	go run ./cmd/kubernetes server --env=dev

worker-kubernetes:
	go run ./cmd/kubernetes worker --env=dev

outbox-kubernetes:
	go run ./cmd/kubernetes outbox --env=dev

migrate-kubernetes:
	go run ./cmd/kubernetes migrate --env=dev

rollback-kubernetes:
	go run ./cmd/kubernetes rollback --env=dev

seed-kubernetes:
	go run ./cmd/kubernetes seed --env=dev

seed-rollback-kubernetes:
	go run ./cmd/kubernetes seed-rollback --env=dev

# Republishes every Plan and Region for the kubernetes context's catalog read
# model (docs/adr/0014); the droplet sync job does the same every run.
replay-catalog:
	go run ./cmd/droplet replay-catalog --env=dev

# For an older postgres volume that predates scripts/init-db.sql's database.
create-db-kubernetes:
	docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'nodera_kubernetes'" | grep -q 1 || \
		docker compose exec -T postgres psql -U postgres -c 'CREATE DATABASE nodera_kubernetes;'

# DESTRUCTIVE: drop and recreate the kubernetes database, then migrate and seed.
reset-db-kubernetes:
	docker compose exec -T postgres psql -U postgres -c 'DROP DATABASE IF EXISTS nodera_kubernetes WITH (FORCE);' -c 'CREATE DATABASE nodera_kubernetes;'
	$(MAKE) -s migrate-kubernetes
	$(MAKE) -s seed-kubernetes

swagger-kubernetes:
	$(LOCAL_BIN)/swag init -q -d ./cmd/kubernetes,./contexts/kubernetes -g server.go -o contexts/kubernetes/docs --parseDependency --parseInternal

# A Region's control plane fleet (docs/adr/0013, deploy/kubernetes/README.md):
# a server ordered from Cherry and turned into a Kamaji management cluster.
# It bills by the hour while it exists; destroy it when done.
#   make k8s-fleet-up [REGION=vno1]        order, install and write the config
#   make k8s-fleet-install [REGION=vno1]   finish or repeat the install
#   make k8s-fleet-config [REGION=vno1]    write the kubernetes dev.toml again
#   make k8s-fleet-status [REGION=vno1]
#   make k8s-fleet-destroy [REGION=vno1]   delete the server at Cherry
k8s-fleet-up k8s-fleet-install k8s-fleet-config k8s-fleet-status k8s-fleet-destroy:
	FLEET_REGION=$(or $(REGION),vno1) ./deploy/kubernetes/fleet/fleet.sh $(patsubst k8s-fleet-%,%,$@)

# =============================================================================
# Integration gateway
# =============================================================================

server-integration-gateway:
	go run ./cmd/integration-gateway server --env=dev

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
	@$(MAKE) -s swagger-mail
	@$(MAKE) -s swagger-storage
	@$(MAKE) -s swagger-kubernetes
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
	@touch $(LOG_DIR)/mail-server.log $(LOG_DIR)/mail-smtp.log $(LOG_DIR)/mail-dns.log $(LOG_DIR)/mail-worker.log $(LOG_DIR)/mail-outbox.log $(LOG_DIR)/integration-gateway.log
	@( go run ./cmd/mail server --env=dev > $(LOG_DIR)/mail-server.log 2>&1 ) & echo $$! > $(PID_DIR)/mail-server.pid
	@( go run ./cmd/mail smtp --env=dev > $(LOG_DIR)/mail-smtp.log 2>&1 ) & echo $$! > $(PID_DIR)/mail-smtp.pid
	@( go run ./cmd/mail dns --env=dev > $(LOG_DIR)/mail-dns.log 2>&1 ) & echo $$! > $(PID_DIR)/mail-dns.pid
	@( go run ./cmd/mail worker --env=dev > $(LOG_DIR)/mail-worker.log 2>&1 ) & echo $$! > $(PID_DIR)/mail-worker.pid
	@( go run ./cmd/mail outbox --env=dev > $(LOG_DIR)/mail-outbox.log 2>&1 ) & echo $$! > $(PID_DIR)/mail-outbox.pid
	@( go run ./cmd/integration-gateway server --env=dev > $(LOG_DIR)/integration-gateway.log 2>&1 ) & echo $$! > $(PID_DIR)/integration-gateway.pid
	@touch $(LOG_DIR)/storage-server.log $(LOG_DIR)/storage-worker.log $(LOG_DIR)/storage-outbox.log
	@( go run ./cmd/storage server --env=dev > $(LOG_DIR)/storage-server.log 2>&1 ) & echo $$! > $(PID_DIR)/storage-server.pid
	@( go run ./cmd/storage worker --env=dev > $(LOG_DIR)/storage-worker.log 2>&1 ) & echo $$! > $(PID_DIR)/storage-worker.pid
	@( go run ./cmd/storage outbox --env=dev > $(LOG_DIR)/storage-outbox.log 2>&1 ) & echo $$! > $(PID_DIR)/storage-outbox.pid
	@touch $(LOG_DIR)/kubernetes-server.log $(LOG_DIR)/kubernetes-worker.log $(LOG_DIR)/kubernetes-outbox.log
	@( go run ./cmd/kubernetes server --env=dev > $(LOG_DIR)/kubernetes-server.log 2>&1 ) & echo $$! > $(PID_DIR)/kubernetes-server.pid
	@( go run ./cmd/kubernetes worker --env=dev > $(LOG_DIR)/kubernetes-worker.log 2>&1 ) & echo $$! > $(PID_DIR)/kubernetes-worker.pid
	@( go run ./cmd/kubernetes outbox --env=dev > $(LOG_DIR)/kubernetes-outbox.log 2>&1 ) & echo $$! > $(PID_DIR)/kubernetes-outbox.pid
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

test-mail-integration:
	INTEGRATION=true go test ./cmd/mail/integration-test/ -count=1 -v

test-integration-gateway:
	INTEGRATION=true go test ./cmd/integration-gateway/ -count=1 -v

test-storage-integration:
	INTEGRATION=true go test ./cmd/storage/integration-test/ -count=1 -v

test-kubernetes-integration:
	INTEGRATION=true go test ./cmd/kubernetes/integration-test/ -count=1 -v

# Pulls the KumoMTA image and runs deploy/kumomta's policy against a real MTA.
test-kumomta-contract:
	NODERA_MAIL_SMOKE=true go test ./contexts/mail/mta/kumomta/ -run TestKumoMTAContract -count=1 -v

# Pulls the Ceph image and runs the adapter against a real single-container cluster.
test-ceph-contract:
	NODERA_STORAGE_SMOKE=true go test ./contexts/storage/engine/ceph/ -run TestCephContract -count=1 -v

# Runs the adapter against a real Kamaji on a Region's fleet
# (deploy/kubernetes/README.md, make k8s-fleet-up), named by the NODERA_KAMAJI_*
# variables; it opens a Control plane there and deletes it again.
test-kamaji-contract:
	NODERA_KUBERNETES_SMOKE=true go test ./contexts/kubernetes/engine/kamaji/ -run TestKamajiContract -count=1 -v

lint:
	$(LOCAL_BIN)/golangci-lint run --config .golangci.yml ./...

format:
	gofmt -s -w .

generate:
	go generate ./...
