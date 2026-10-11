FROM ghcr.io/astral-sh/uv:0.12.20 AS uv
FROM python:3.12-slim

COPY --from=uv /uv /usr/local/bin/uv
RUN apt-get update && apt-get install -y --no-install-recommends libgomp1 libgl1 libglib2.0-0 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY services/ocr/pyproject.toml services/ocr/uv.lock ./
RUN uv sync --frozen --no-dev
COPY services/ocr/app.py ./
RUN useradd --create-home --uid 10001 horse && chown -R 10001:10001 /app
USER horse
EXPOSE 8000
CMD ["/app/.venv/bin/uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
