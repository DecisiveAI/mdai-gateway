DOCKER_TAG ?= 0.1.5
CHART_VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
REPO_NAME := $(shell basename -s .git `git config --get remote.origin.url`)
GO_TEST := CGO_ENABLED=0 go test -count=1

docker-login docker-build docker-push: AWS_ECR_REPO := public.ecr.aws/p3k6k6h3
docker-build docker-push: DOCKER_IMAGE := $(AWS_ECR_REPO)/$(REPO_NAME):$(DOCKER_TAG)

.PHONY: docker-login
docker-login:
	aws ecr-public get-login-password | docker login --username AWS --password-stdin $(AWS_ECR_REPO)

.PHONY: docker-build
docker-build: tidy vendor
	docker buildx build --platform linux/arm64,linux/amd64 -t $(DOCKER_IMAGE) . --load

.PHONY: docker-push
docker-push: tidy vendor docker-login
	docker buildx build --platform linux/arm64,linux/amd64 -t $(DOCKER_IMAGE) . --push

.PHONY: build
build: tidy vendor
	CGO_ENABLED=0 go build -ldflags="-w -s" -o mdai-gateway ./cmd/mdai-gateway

.PHONY: test
test: tidy vendor
	$(GO_TEST) ./...

.PHONY: testv
testv: tidy vendor
	$(GO_TEST) -v ./...

.PHONY: cover
cover: tidy vendor
	$(GO_TEST) -cover ./...

.PHONY: coverv
coverv: tidy vendor
	$(GO_TEST) -v -cover ./...

.PHONY: coverhtml
coverhtml:
	@trap 'rm -f coverage.out' EXIT; \
	go test -count=1 -coverprofile=coverage.out ./... && \
	go tool cover -html=coverage.out -o coverage.html && \
	( open coverage.html || xdg-open coverage.html )

.PHONY: clean-coverage
clean-coverage:
	rm -f coverage.out coverage.html

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: tidy-check
tidy-check: tidy
	@git diff --quiet --exit-code go.mod go.sum || { echo >&2 "go.mod or go.sum is out of sync. Run 'make tidy'."; exit 1; }

.PHONY: vendor
vendor:
	@go mod vendor

.PHONY: helm
helm:
	@echo "Usage: make helm-<command>"
	@echo "Available commands:"
	@echo "  helm-package   Package the Helm chart"
	@echo "  helm-publish   Publish the Helm chart"

.PHONY: helm-package
helm-package: CHART_DIR := ./deployment
helm-package:
	@echo "📦 Packaging Helm chart..."
	@helm package -u --version $(CHART_VERSION) --app-version $(CHART_VERSION) $(CHART_DIR) > /dev/null

.PHONY: helm-publish
helm-publish: CHART_NAME := $(REPO_NAME)
helm-publish: CHART_REPO := git@github.com:DecisiveAI/mdai-helm-charts.git
helm-publish: CHART_PACKAGE := $(CHART_NAME)-$(CHART_VERSION).tgz
helm-publish: BASE_BRANCH := gh-pages
helm-publish: TARGET_BRANCH := $(CHART_NAME)-v$(CHART_VERSION)
helm-publish: CLONE_DIR := $(shell mktemp -d /tmp/mdai-helm-charts.XXXXXX)
helm-publish: REPO_DIR := $(shell pwd)
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