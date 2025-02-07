FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH
ENV GOPRIVATE=github.com/decisiveai/mdai-operator
WORKDIR /opt/event-handler-webservice

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath -ldflags="-w -s" -o /event-handler-webservice main.go

FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=builder /event-handler-webservice /event-handler-webservice
CMD ["/event-handler-webservice"]
