FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /opt/event-handler-webservice

RUN apt-get update && apt-get install -y ca-certificates git-core ssh
RUN echo "Host github.com\n\tStrictHostKeyChecking no\n" >> /root/.ssh/config
RUN git config --global url.ssh://git@github.com/.insteadOf https://github.com/

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /event-handler-webservice main.go

FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=builder /event-handler-webservice /event-handler-webservice
CMD ["/event-handler-webservice"]
