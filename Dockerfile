# syntax=docker/dockerfile:1
# 1) PWA con Node  2) binario Go con la PWA incorporata  3) distroless/static, utente non root

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
ARG VERSION=dev
RUN VITE_APP_VERSION=${VERSION} npm run build

FROM golang:1.24-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/embed.go
COPY --from=web /src/web/dist web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/toigoweb/totem/internal/version.Version=${VERSION}" \
      -o /out/totem ./cmd/totem \
 && mkdir -p /out/data /out/secrets

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/totem /totem
COPY --from=go --chown=65532:65532 /out/data /data
COPY --from=go --chown=65532:65532 /out/secrets /secrets
USER 65532:65532
EXPOSE 8080
VOLUME ["/data", "/secrets"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["/totem", "healthcheck"]
ENTRYPOINT ["/totem"]
CMD ["serve"]
