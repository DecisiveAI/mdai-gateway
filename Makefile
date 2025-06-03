DOCKER_TAG ?= latest
LATEST_TAG := $(shell git tag --sort=-v:refname | head -n1 | sed 's/^v//')
CHART_VERSION ?= $(LATEST_TAG)
CHART_DIR := ./deployment
CHART_NAME := mdai-gateway
CHART_PACKAGE := $(CHART_NAME)-$(CHART_VERSION).tgz
CHART_REPO := git@github.com:DecisiveAI/mdai-helm-charts.git
BASE_BRANCH := gh-pages
TARGET_BRANCH := $(CHART_NAME)-v$(CHART_VERSION)
CLONE_DIR := $(shell mktemp -d /tmp/mdai-helm-charts.XXXXXX)
REPO_DIR := $(shell pwd)
ECR_REPO := public.ecr.aws/p3k6k6h3

TEST_PKGS := $(shell go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./...)
GO_TEST_FLAGS := -mod=vendor -count=1
GO_BENCH_FLAGS := -mod=vendor -bench=. -benchmem
GOLANGCI_LINT_FLAGS :=

.PHONY: docker-login
docker-login:
	aws ecr-public get-login-password | docker login --username AWS --password-stdin $(ECR_REPO)

.PHONY: docker-build
docker-build: tidy vendor
	docker buildx build --platform linux/arm64,linux/amd64 -t $(ECR_REPO)/mdai-gateway:$(DOCKER_TAG) . --load

.PHONY: docker-push
docker-push: tidy vendor docker-login
	docker buildx build --platform linux/arm64,linux/amd64 -t $(ECR_REPO)/mdai-gateway:$(DOCKER_TAG) . --push

.PHONY: build
build: tidy vendor
	@CGO_ENABLED=0 go build -mod=vendor -ldflags="-w -s" -o mdai-gateway ./cmd/mdai-gateway

.PHONY: test
test: tidy vendor
	@CGO_ENABLED=0 go test $(GO_TEST_FLAGS) $(TEST_PKGS)

.PHONY: testv
testv: GO_TEST_FLAGS += -v
testv: test

.PHONY: bench
bench: tidy vendor
	@CGO_ENABLED=0 go test $(GO_BENCH_FLAGS) $(TEST_PKGS)

.PHONY: benchv
benchv: GO_BENCH_FLAGS += -v
benchv: bench

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: vendor
vendor:
	@go mod vendor

.PHONY: lint
lint: tidy vendor
	@golangci-lint run $(GOLANGCI_LINT_FLAGS)

.PHONY: lintv
lintv: GOLANGCI_LINT_FLAGS += -v
lintv: lint

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
