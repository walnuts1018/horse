# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24.22.0-slim AS web-deps

WORKDIR /src/web

RUN corepack enable && corepack prepare pnpm@12.11.2 --activate

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=horse-pnpm,target=/pnpm/store,sharing=locked \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store

FROM --platform=$BUILDPLATFORM node:24.22.0-slim AS web-build

WORKDIR /src/web

RUN corepack enable && corepack prepare pnpm@12.11.2 --activate

COPY --from=web-deps /src/web/node_modules ./node_modules
COPY web/ ./
COPY graphql/ /src/graphql/
RUN pnpm build

FROM rust:1.99.0-bookworm AS rust-build

WORKDIR /src

ARG TARGETARCH
ENV CARGO_TARGET_DIR=/target/${TARGETARCH}

RUN apt-get update && apt-get install -y --no-install-recommends libpq-dev && rm -rf /var/lib/apt/lists/*

COPY Cargo.toml Cargo.lock ./
COPY apps/ apps/
COPY adapters/ adapters/
COPY contexts/ contexts/
COPY platform/ platform/
COPY rust-migrations/ rust-migrations/
COPY atlas-rust.hcl ./

RUN --mount=type=cache,id=horse-cargo-registry,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,id=horse-cargo-git,target=/usr/local/cargo/git,sharing=locked \
    --mount=type=cache,id=horse-cargo-target-${TARGETARCH},target=/target,sharing=locked \
    cargo build --locked --release -p horse-api -p horse-worker \
    && mkdir -p /out \
    && cp "${CARGO_TARGET_DIR}/release/horse-api" /out/api \
    && cp "${CARGO_TARGET_DIR}/release/horse-worker" /out/worker

FROM docker.io/arigaio/atlas:1.3.4-distroless AS atlas

FROM debian:bookworm-slim AS rust-runtime-base

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libpq5 && rm -rf /var/lib/apt/lists/*

FROM gcr.io/distroless/nodejs24-debian13:nonroot AS web-runtime

ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000

WORKDIR /app

COPY --from=web-build --chown=10001:10001 /src/web/.output/ ./.output/

USER 10001:10001

EXPOSE 3000

CMD [".output/server/index.mjs"]

FROM rust-runtime-base AS rust-runtime

WORKDIR /app

COPY --from=rust-build --chown=65532:65532 /out/api ./api
COPY --from=rust-build --chown=65532:65532 /out/worker ./worker
COPY --from=atlas --chown=65532:65532 --chmod=0555 /atlas /usr/local/bin/atlas
COPY --chown=65532:65532 atlas-rust.hcl ./atlas-rust.hcl
COPY --chown=65532:65532 rust-migrations/ ./rust-migrations/

USER 65532:65532

EXPOSE 8080 8081

CMD ["/app/api"]
