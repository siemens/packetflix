
.PHONY: help deploy undeploy dist

help: ## list available targets
	@# Derived from Gomega's Makefile (github.com/onsi/gomega) under MIT License
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

deploy: ## deploy Packetflix service exposed on host port 5001
	scripts/docker-build.sh deployments/packetflix/Dockerfile \
		-t packetflix
	docker compose -p packetflix -f deployments/packetflix/docker-compose.yaml up

undeploy: ## remove any Packetflix service deployment
	docker compose -p packetflix -f deployments/packetflix/docker-compose.yaml down

dist: ## build multi-arch image (amd64, arm64) and push to local running registry on port 5999.
	scripts/multiarch-builder.sh
