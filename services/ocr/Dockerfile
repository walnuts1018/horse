# syntax=docker/dockerfile:1

FROM ghcr.io/astral-sh/uv:0.13.1@sha256:4a1f3666816dafadd20ceb71eac9a7d56d7ef30279f7d64048519ab901ada336 AS uv

FROM python:3.13-slim@sha256:e48f59cdba69a3cbd9ed2cf0f348d90d45a8d350d98134cf1070bc106ee9ed90 AS runtime-base

RUN rm -f /etc/apt/apt.conf.d/docker-clean \
    && printf '%s\n' 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,id=horse-ocr-apt-cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,id=horse-ocr-apt-lib,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends libgomp1 libgl1 libglib2.0-0

RUN useradd --create-home --uid 10001 horse

FROM runtime-base AS dependencies

COPY --from=uv /uv /usr/local/bin/uv

WORKDIR /app

ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1

COPY services/ocr/pyproject.toml services/ocr/uv.lock ./
RUN --mount=type=cache,id=horse-ocr-uv,target=/root/.cache/uv,sharing=locked \
    uv sync --frozen --no-dev

FROM runtime-base AS runtime

WORKDIR /app

COPY --from=dependencies --chown=10001:10001 /app/.venv ./.venv
COPY --chown=10001:10001 services/ocr/app.py ./

USER 10001:10001

EXPOSE 8000

CMD ["/app/.venv/bin/uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
