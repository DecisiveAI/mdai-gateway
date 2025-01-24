FROM golang:1.23-bookworm AS builder
WORKDIR /opt/event-handler-webservice
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /event-handler-webservice main.go

FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=builder /event-handler-webservice /event-handler-webservice
CMD ["/event-handler-webservice"]
