FROM node:24.21.0-slim AS web-build

WORKDIR /src/web

RUN corepack enable && corepack prepare pnpm@11.28.2 --activate

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
COPY graphql/ /src/graphql/
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS api-build

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api \
    && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/worker ./cmd/worker

FROM docker.io/arigaio/atlas:1.3.0 AS atlas

FROM node:24.21.0-slim AS runtime

ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000

WORKDIR /app

COPY --from=web-build --chown=10001:10001 /src/web/.output/ ./.output/
COPY --from=api-build --chown=10001:10001 /out/api ./api
COPY --from=api-build --chown=10001:10001 /out/worker ./worker
COPY --from=atlas --chown=10001:10001 /atlas /usr/local/bin/atlas
COPY --chown=10001:10001 atlas.hcl ./atlas.hcl
COPY --chown=10001:10001 migrations/ ./migrations/

USER 10001:10001

EXPOSE 3000 8080 8081

CMD ["node", ".output/server/index.mjs"]
