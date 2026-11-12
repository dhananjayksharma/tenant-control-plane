# Tenant Provisioning Control Plane — local developer commands.
# Requirements: Docker + Compose v2. Local Go 1.23+ for the optional host-only targets.
.DEFAULT_GOAL := help
.PHONY: help up build down reset restart ps logs logs-api logs-worker fmt fmt-check lint unit integration test check failure-worker restore-worker health

COMPOSE := docker compose
# Explicitly target the existing Minikube profile; never rely on the active kube context.
MINIKUBE_PROFILE ?= gpu-cpu-lab
MINIKUBE := minikube -p $(MINIKUBE_PROFILE)
KUBECTL := $(MINIKUBE) kubectl --
K8S_NAMESPACE := tenant-control-plane

help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "; print "Usage: make <target>\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

up: ## Build and start API, database, broker, and worker
	$(COMPOSE) up --build -d

build: ## Build the API and worker container images
	$(COMPOSE) build api worker

down: ## Stop containers (preserve database volume)
	$(COMPOSE) down

reset: ## WARNING: Stop containers and delete persistent database data
	$(COMPOSE) down -v

restart: ## Restart the API and worker
	$(COMPOSE) restart api worker

ps: ## Show container status
	$(COMPOSE) ps

logs: ## Follow API and worker logs
	$(COMPOSE) logs -f api worker

logs-api: ## Follow API logs
	$(COMPOSE) logs -f api

logs-worker: ## Follow worker logs
	$(COMPOSE) logs -f worker

fmt: ## Format Go source (requires locally installed Go)
	gofmt -w $$(find cmd internal scripts tests -name '*.go' -type f)

fmt-check: ## Verify Go formatting in the test container
	$(COMPOSE) --profile test run --rm --no-deps --entrypoint sh test -c 'files=$$(gofmt -l cmd internal scripts tests); if [ -n "$$files" ]; then printf "Unformatted files:\n%s\n" "$$files"; exit 1; fi'

lint: ## Run go vet in the test container
	$(COMPOSE) --profile test run --rm --no-deps --entrypoint sh test -c 'go vet ./...'

unit: ## Run Go unit tests with the race detector in a container
	$(COMPOSE) --profile test run --rm --no-deps --entrypoint go test ./... -race -count=1

integration: up ## Run real API + worker integration tests
	$(COMPOSE) --profile test run --rm test

test: integration ## Full project test suite (vet, unit/race, integration/race)

check: fmt-check test ## Check formatting and run the full suite

failure-worker: ## Replace worker with a guaranteed-failure simulator (creates new container)
	$(COMPOSE) stop worker
	$(COMPOSE) run --rm -d --name tenant-fail-worker worker --fail-rate=1 --min-delay-ms=200 --max-delay-ms=500

restore-worker: ## Restore normal worker; remove temporary failure worker if present
	- docker rm -f tenant-fail-worker
	$(COMPOSE) up -d worker

health: ## Check API health from host
	curl -fsS http://localhost:8080/healthz
	@printf '\n'

# Optional Kubernetes / Minikube deployment (Docker Compose remains the assignment boot command).
.PHONY: k8s-profile k8s-build k8s-secrets k8s-up k8s-status k8s-health k8s-logs k8s-port-forward k8s-failure-worker k8s-restore-worker k8s-down k8s-wipe

k8s-profile: ## Show status of the selected Minikube profile
	$(MINIKUBE) status

k8s-build: k8s-profile ## Build API/worker locally and load images into Minikube
	docker build --build-arg CMD=api -t tenant-control-plane-api:dev .
	docker build --build-arg CMD=worker -t tenant-control-plane-worker:dev .
	$(MINIKUBE) image load tenant-control-plane-api:dev
	$(MINIKUBE) image load tenant-control-plane-worker:dev

k8s-secrets: ## Create Kubernetes Secret (export DB_PASSWORD and RABBITMQ_PASSWORD first)
	MINIKUBE_PROFILE="$(MINIKUBE_PROFILE)" bash scripts/minikube-secrets.sh

k8s-up: k8s-build k8s-secrets ## Build images, create runtime secrets, deploy to Minikube
	$(KUBECTL) apply -k k8s/
	$(KUBECTL) -n $(K8S_NAMESPACE) rollout status statefulset/postgres --timeout=180s
	$(KUBECTL) -n $(K8S_NAMESPACE) rollout status statefulset/rabbitmq --timeout=180s
	$(KUBECTL) -n $(K8S_NAMESPACE) rollout status deployment/tenant-api --timeout=180s
	$(KUBECTL) -n $(K8S_NAMESPACE) rollout status deployment/tenant-worker --timeout=180s

k8s-status: ## Display Kubernetes resources and PVCs
	$(KUBECTL) -n $(K8S_NAMESPACE) get pods,svc,pvc

k8s-health: ## Check API health using port-forward (run k8s-port-forward in another terminal)
	curl -fsS http://localhost:8080/healthz
	@printf '\n'

k8s-logs: ## Follow worker logs
	$(KUBECTL) -n $(K8S_NAMESPACE) logs -f deploy/tenant-worker

k8s-port-forward: ## Expose API at localhost:8080 (foreground)
	$(KUBECTL) -n $(K8S_NAMESPACE) port-forward svc/api 8080:8080

k8s-failure-worker: ## Force new tasks to fail (restart worker with 100% failure)
	$(KUBECTL) -n $(K8S_NAMESPACE) patch deployment tenant-worker --type=strategic -p '{"spec":{"template":{"spec":{"containers":[{"name":"worker","args":["--fail-rate=1","--min-delay-ms=200","--max-delay-ms=500"]}]}}}}'
	$(KUBECTL) -n $(K8S_NAMESPACE) rollout status deployment/tenant-worker --timeout=120s

k8s-restore-worker: ## Restore successful worker configuration
	$(KUBECTL) -n $(K8S_NAMESPACE) patch deployment tenant-worker --type=strategic -p '{"spec":{"template":{"spec":{"containers":[{"name":"worker","args":["--fail-rate=0","--min-delay-ms=300","--max-delay-ms=900"]}]}}}}'
	$(KUBECTL) -n $(K8S_NAMESPACE) rollout status deployment/tenant-worker --timeout=120s

k8s-down: ## Delete workload manifests (PVCs are retained)
	$(KUBECTL) delete -k k8s/ --ignore-not-found

k8s-wipe: ## DANGER: delete whole namespace, including Secrets and PVCs
	$(KUBECTL) delete namespace $(K8S_NAMESPACE) --ignore-not-found
