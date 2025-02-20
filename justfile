VERSION := env_var_or_default("VERSION", "latest")

default:
    @just --list --justfile {{justfile()}}

docker-login:
	@aws ecr-public get-login-password | docker login --username AWS --password-stdin public.ecr.aws/p3k6k6h3

docker-build: tidy vendor
	@docker buildx build --platform linux/arm64,linux/amd64 -t public.ecr.aws/p3k6k6h3/event-handler-webservice:{{VERSION}} . --load

docker-push: tidy vendor docker-login
	@docker buildx build --platform linux/arm64,linux/amd64 -t public.ecr.aws/p3k6k6h3/event-handler-webservice:{{VERSION}} . --push

build: tidy vendor
	@CGO_ENABLED=0 go build -mod=vendor -ldflags="-w -s" -o event-handler-webservice main.go

test: tidy vendor
	@CGO_ENABLED=0 go test -mod=vendor -v -count=1 ./...

tidy:
	@go mod tidy

vendor:
	@go mod vendor
