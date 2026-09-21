FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/server/web
COPY server/web/package.json server/web/package-lock.json ./
RUN npm ci
COPY server/web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/webui/dist ./internal/webui/dist

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/hho ./cmd/hho
RUN mkdir -p /data
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=65532:65532 /out/hho /usr/local/bin/hho
COPY --from=build --chown=65532:65532 /data /data

VOLUME ["/data"]
EXPOSE 7745
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/hho", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/hho"]
CMD ["serve"]
