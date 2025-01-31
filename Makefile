VERSION ?= latest

.PHONY: docker-login
.SILENT: docker-login
docker-login:
	aws ecr-public get-login-password | docker login --username AWS --password-stdin public.ecr.aws/p3k6k6h3

.PHONY: docker-build
.SILENT: docker-build
docker-build: tidy vendor
	docker buildx build --platform linux/arm64,linux/amd64 -t public.ecr.aws/p3k6k6h3/event-handler-webservice:$(VERSION) . --load

.PHONY: docker-push
.SILENT: docker-push
docker-push: tidy vendor docker-login
	docker buildx build --platform linux/arm64,linux/amd64 -t public.ecr.aws/p3k6k6h3/event-handler-webservice:$(VERSION) . --push

.PHONY: build
.SILENT: build
build: tidy vendor
	CGO_ENABLED=0 go build -mod=vendor -ldflags="-w -s" -o event-handler-webservice main.go

.PHONY: test
.SILENT: test
test: tidy vendor
	CGO_ENABLED=0 go test -mod=vendor -v -count=1 ./...

.PHONY: tidy
.SILENT: tidy
tidy:
	go mod tidy

.PHONY: vendor
.SILENT: vendor
vendor:
	go mod vendor
