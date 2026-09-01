# Asiakirjat — common tasks.
#
# The headline is `make demo`: one call from a fresh clone to a running
# instance you can log into. Everything else is the day-to-day.
#
# Override any variable on the command line, e.g. `make demo PORT=9000`.

SHELL := /bin/sh

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# A stable tag on purpose: this is a playground image, rebuilt often, and
# tagging it by version would leave a pile of near-identical images behind.
# VERSION is still stamped into the binary, so /healthz and the footer report it.
IMAGE       ?= asiakirjat:local
PORT        ?= 8080
CONTAINER   ?= asiakirjat-demo
VOLUME      ?= asiakirjat-demo-data
ADMIN_USER  ?= admin

# The demo admin password is generated once and kept, because the admin user is
# only created on the container's first start. Regenerating it on every run
# would print credentials that stopped working after the first `make demo`.
PASSWORD_FILE := .demo-password

GO      ?= go
GOFLAGS ?= -mod=vendor
LDFLAGS  = -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help
.PHONY: help build test test-go test-js run image demo logs stop reset config compose-up compose-down clean

help: ## Show this help
	@echo "Asiakirjat $(VERSION)"
	@echo
	@echo "Quick start:  make demo"
	@echo
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo
	@echo "Variables:    PORT=$(PORT)  IMAGE=$(IMAGE)"

## --- Local development ------------------------------------------------------

build: ## Build the binary into ./asiakirjat
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o asiakirjat .

test: test-go test-js ## Run every test suite

test-go: ## Run the Go tests
	$(GO) test $(GOFLAGS) -count=1 ./...

test-js: ## Run the overlay JS tests (jsdom)
	@if [ -f package.json ]; then \
		npm_config_cache=$${npm_config_cache:-/tmp/npmcache} npm ci --silent && \
		npm_config_cache=$${npm_config_cache:-/tmp/npmcache} npm test; \
	fi

run: build $(PASSWORD_FILE) ## Build and run locally, no Docker
	@echo "Starting on http://localhost:$(PORT) — admin / $$(cat $(PASSWORD_FILE))"
	@ASIAKIRJAT_SERVER_PORT=$(PORT) \
	 ASIAKIRJAT_ADMIN_USERNAME=$(ADMIN_USER) \
	 ASIAKIRJAT_ADMIN_PASSWORD="$$(cat $(PASSWORD_FILE))" \
	 ./asiakirjat -config config.yaml

## --- Docker -----------------------------------------------------------------

image: ## Build the Docker image
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

demo: image $(PASSWORD_FILE) ## Build the image and run it, ready to log into
	@docker rm -f $(CONTAINER) >/dev/null 2>&1 || true
	@docker run -d --name $(CONTAINER) \
		-p $(PORT):8080 \
		-v $(VOLUME):/app/data \
		-e ASIAKIRJAT_ADMIN_USERNAME=$(ADMIN_USER) \
		-e ASIAKIRJAT_ADMIN_PASSWORD="$$(cat $(PASSWORD_FILE))" \
		$(IMAGE) >/dev/null
	@printf 'Waiting for it to come up'
	@i=0; until curl -sf -o /dev/null http://localhost:$(PORT)/healthz; do \
		i=$$((i+1)); \
		if [ $$i -gt 60 ]; then \
			echo; echo "It did not become healthy. Logs:"; docker logs $(CONTAINER); exit 1; \
		fi; \
		printf '.'; sleep 1; \
	done
	@echo
	@echo
	@echo "  Asiakirjat is running."
	@echo
	@echo "    URL:       http://localhost:$(PORT)"
	@echo "    Username:  $(ADMIN_USER)"
	@echo "    Password:  $$(cat $(PASSWORD_FILE))"
	@echo
	@echo "  Deploy the built-in docs from Admin > Deploy Built-in Docs to have"
	@echo "  something to read."
	@echo
	@echo "    make logs    follow the log"
	@echo "    make stop    stop it, keeping its data"
	@echo "    make reset   stop it and delete its data"
	@echo

logs: ## Follow the demo container's log
	docker logs -f $(CONTAINER)

stop: ## Stop and remove the demo container, keeping its data
	@docker rm -f $(CONTAINER) >/dev/null 2>&1 || true
	@echo "Stopped. Its data is kept in the '$(VOLUME)' volume; 'make demo' brings it back."

reset: stop ## Stop the demo and delete its data and password
	@docker volume rm $(VOLUME) >/dev/null 2>&1 || true
	@rm -f $(PASSWORD_FILE)
	@echo "Data and password removed. The next 'make demo' starts clean."

## --- docker compose ---------------------------------------------------------

config: config.yaml ## Create config.yaml from the example, if absent

config.yaml:
	@cp config.yaml.example config.yaml
	@echo "Wrote config.yaml from the example."
	@echo "Set auth.initial_admin.password to something at least 12 characters —"
	@echo "the server refuses to create an admin with a weak or default password."

compose-up: config ## Start with docker compose, using ./config.yaml
	docker compose up --build -d
	@echo "Running on http://localhost:8080 with the credentials in config.yaml."

compose-down: ## Stop the docker compose stack
	docker compose down

## --- Housekeeping -----------------------------------------------------------

clean: ## Remove build artifacts
	rm -f asiakirjat

# Generated on first use and kept: the admin account is created only on a
# container's first start, so a fresh password each run would be a lie.
$(PASSWORD_FILE):
	@(openssl rand -base64 18 2>/dev/null || head -c 18 /dev/urandom | base64) | tr -d '\n/+=' > $@
	@chmod 600 $@
