
VERSION := $(shell git describe --tags --always --dirty)

.PHONY: help deploy undeploy dist vuln

help: ## list available targets
	@# Derived from Gomega's Makefile (github.com/onsi/gomega) under MIT License
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

deploy: ## build and deploy Packetflix service exposed on host port 5001
	VERSION=$(VERSION) docker compose -p packetflix -f deployments/packetflix/docker-compose.yaml up --build

undeploy: ## remove any Packetflix service deployment
	docker compose -p packetflix -f deployments/packetflix/docker-compose.yaml down
