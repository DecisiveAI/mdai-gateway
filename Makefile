DOCKER_TAG ?= latest
LATEST_TAG := $(shell git describe --tags --abbrev=0 $(git rev-parse --abbrev-ref HEAD) | sed 's/^v//')
CHART_VERSION ?= $(LATEST_TAG)
CHART_DIR := ./deployment
CHART_NAME := event-handler-webservice
CHART_PACKAGE := $(CHART_NAME)-$(CHART_VERSION).tgz
CHART_REPO := git@github.com:DecisiveAI/mdai-helm-charts.git
BASE_BRANCH := gh-pages
TARGET_BRANCH := $(CHART_NAME)-v$(CHART_VERSION)
CLONE_DIR := $(shell mktemp -d /tmp/mdai-helm-charts.XXXXXX)
REPO_DIR := $(shell pwd)

.PHONY: docker-login
docker-login:
	aws ecr-public get-login-password | docker login --username AWS --password-stdin public.ecr.aws/p3k6k6h3

.PHONY: docker-build
docker-build: tidy vendor
	docker buildx build --platform linux/arm64,linux/amd64 -t public.ecr.aws/p3k6k6h3/event-handler-webservice:$(DOCKER_TAG) . --load

.PHONY: docker-push
docker-push: tidy vendor docker-login
	docker buildx build --platform linux/arm64,linux/amd64 -t public.ecr.aws/p3k6k6h3/event-handler-webservice:$(DOCKER_TAG) . --push

.PHONY: build
build: tidy vendor
	CGO_ENABLED=0 go build -mod=vendor -ldflags="-w -s" -o event-handler-webservice .

.PHONY: test
test: tidy vendor
	CGO_ENABLED=0 go test -mod=vendor -v -count=1 ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: vendor
vendor:
	go mod vendor

.PHONY: helm
helm:
	@echo "Usage: make helm-<command>"
	@echo "Available commands:"
	@echo "  helm-package   Package the Helm chart"
	@echo "  helm-publish   Publish the Helm chart"

.PHONY: helm-package
helm-package:
	@echo "📦 Packaging Helm chart..."
	@helm package -u --version $(CHART_VERSION) --app-version $(CHART_VERSION) $(CHART_DIR) > /dev/null

.PHONY: helm-publish
helm-publish: helm-package
	@echo "🚀 Cloning $(CHART_REPO)..."
	@rm -rf $(CLONE_DIR)
	@git clone -q --branch $(BASE_BRANCH) $(CHART_REPO) $(CLONE_DIR)

	@echo "🌿 Creating branch $(TARGET_BRANCH) from $(BASE_BRANCH)..."
	@cd $(CLONE_DIR) && git checkout -q -b $(TARGET_BRANCH)

	@echo "📤 Copying and indexing chart..."
	@cd $(CLONE_DIR) && \
		helm repo index $(REPO_DIR) --merge index.yaml && \
		mv $(REPO_DIR)/$(CHART_PACKAGE) $(CLONE_DIR)/ && \
		mv $(REPO_DIR)/index.yaml $(CLONE_DIR)/

	@echo "🚀 Committing changes..."
	@cd $(CLONE_DIR) && \
		git add $(CHART_PACKAGE) index.yaml && \
		git commit -q -m "chore: publish $(CHART_PACKAGE)" && \
		git push -q origin $(TARGET_BRANCH) && \
		rm -rf $(CLONE_DIR)

	@echo "✅ Chart published"