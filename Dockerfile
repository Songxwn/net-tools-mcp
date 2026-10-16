FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/net-tools-mcp .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl iputils-ping \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/net-tools-mcp /usr/local/bin/net-tools-mcp

ENV MCP_LISTEN=:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/net-tools-mcp"]
