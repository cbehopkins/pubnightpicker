# Build from the repository root: last_orders uses relative replace directives to sibling modules.
FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY cellar/go.mod cellar/go.sum ./cellar/
COPY durable_email/go.mod durable_email/go.sum ./durable_email/
COPY email_clients/go.mod ./email_clients/
COPY last_orders/go.mod last_orders/go.sum ./last_orders/

WORKDIR /src/last_orders
RUN go mod download

WORKDIR /src
COPY cellar ./cellar
COPY durable_email ./durable_email
COPY email_clients ./email_clients
COPY last_orders ./last_orders

WORKDIR /src/last_orders
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
	go build -trimpath -ldflags="-s -w" -o /out/last-orders ./cmd/last-orders \
	&& mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/last-orders /usr/local/bin/last-orders
COPY --from=builder --chown=nonroot:nonroot /out/data /data

VOLUME /data
EXPOSE 8080

ENTRYPOINT [ "/usr/local/bin/last-orders" ]
CMD [ "-db-path", "/data/last-orders.db" ]
