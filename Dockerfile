FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /opt/mdai-gateway

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build --trimpath -ldflags="-w -s" -o /mdai-gateway ./cmd/mdai-gateway

FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=builder /mdai-gateway /mdai-gateway
CMD ["/mdai-gateway"]
