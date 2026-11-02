
VERSION := $(shell git describe --tags --always --dirty)
DOCKERFILE := deployments/packetflix/Dockerfile
TRIVY_IMAGE := aquasec/trivy:0.76.1@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c

.PHONY: help deploy undeploy dist vuln bump-debian

help: ## list available targets
	@# Derived from Gomega's Makefile (github.com/onsi/gomega) under MIT License
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

deploy: ## build and deploy Packetflix service exposed on host port 5001
	VERSION=$(VERSION) docker compose -p packetflix -f deployments/packetflix/docker-compose.yaml up --build

undeploy: ## remove any Packetflix service deployment
	docker compose -p packetflix -f deployments/packetflix/docker-compose.yaml down

# Trivy exits with 3 only when it finds fixable vulnerabilities, so that other
# failures still abort.
bump-debian: ## list Debian security fixes missing from the image, then bump the Debian snapshot
	@set -e; \
	tar=$$(mktemp); trap 'rm -f "$$tar"' EXIT; \
	scan() { \
		docker buildx build -q -f $(DOCKERFILE) --load -t packetflix:bump-debian . >/dev/null && \
		docker save packetflix:bump-debian -o "$$tar" && \
		docker run --rm -v "$$tar:/image.tar:ro" -v packetflix-trivy-cache:/root/.cache $(TRIVY_IMAGE) \
			image --quiet --input /image.tar --scanners vuln --pkg-types os --ignore-unfixed --exit-code 3; \
	}; \
	echo "Scanning image with Debian snapshot $$(sed -n 's/^ARG DEBIAN_SNAPSHOT=//p' $(DOCKERFILE))..."; \
	scan || [ $$? -eq 3 ]; \
	snapshot=$$(date -u +%Y%m%dT%H%M%SZ); \
	sed -i "s/^ARG DEBIAN_SNAPSHOT=.*/ARG DEBIAN_SNAPSHOT=$$snapshot/" $(DOCKERFILE); \
	echo "Bumped to Debian snapshot $$snapshot, rescanning..."; \
	scan && echo "No Debian security fixes missing."
