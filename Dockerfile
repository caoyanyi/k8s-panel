FROM node:24.16.0-alpine3.23 AS web-build

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.5-alpine3.23 AS go-build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/panel ./cmd/panel
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/panelctl ./cmd/panelctl

FROM alpine:3.23.3

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 panel \
    && adduser -S -D -H -u 10001 -G panel panel \
    && mkdir -p /app/web /data \
    && chown -R panel:panel /app /data

WORKDIR /app
COPY --from=go-build --chown=panel:panel /out/panel /out/panelctl /app/
COPY --from=web-build --chown=panel:panel /src/web/dist/ /app/web/

ENV PANEL_LISTEN_ADDR=0.0.0.0:8080 \
    PANEL_DATA_FILE=/data/panel.json \
    PANEL_WEB_DIR=/app/web

USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -T 2 -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/panel"]
