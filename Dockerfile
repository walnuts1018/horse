# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24.21.0-slim AS web-deps

WORKDIR /src/web

RUN corepack enable && corepack prepare pnpm@11.28.2 --activate

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=horse-pnpm,target=/pnpm/store,sharing=locked \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store

FROM --platform=$BUILDPLATFORM node:24.21.0-slim AS web-build

WORKDIR /src/web

RUN corepack enable && corepack prepare pnpm@11.28.2 --activate

COPY --from=web-deps /src/web/node_modules ./node_modules
COPY web/ ./
COPY graphql/ /src/graphql/
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS api-build

WORKDIR /src

ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN --mount=type=cache,id=horse-go-mod,target=/go/pkg/mod,sharing=locked \
    go mod download

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=bind,source=.,target=/src,ro \
    --mount=type=cache,id=horse-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=horse-go-build,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out \
    && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api \
    && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/worker ./cmd/worker

FROM docker.io/arigaio/atlas:1.3.0-distroless AS atlas

FROM gcr.io/distroless/base-debian12:nonroot AS go-runtime

WORKDIR /app

COPY --from=api-build --chown=65532:65532 /out/api ./api
COPY --from=api-build --chown=65532:65532 /out/worker ./worker
COPY --from=atlas --chown=65532:65532 --chmod=0555 /atlas /usr/local/bin/atlas
COPY --chown=65532:65532 atlas.hcl ./atlas.hcl
COPY --chown=65532:65532 migrations/ ./migrations/

USER 65532:65532

EXPOSE 8080 8081

CMD ["/app/api"]

FROM gcr.io/distroless/nodejs24-debian13:nonroot AS web-runtime

ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000

WORKDIR /app

COPY --from=web-build --chown=10001:10001 /src/web/.output/ ./.output/

USER 10001:10001

EXPOSE 3000

CMD [".output/server/index.mjs"]
