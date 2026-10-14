# Build context: repository root.
#   docker build -f apps/reasoner/Dockerfile .
FROM python:3.13-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
# The reasoner resolves packages/nodes relative to its source tree, so keep the
# repo layout and install it in editable mode.
WORKDIR /app/apps/reasoner
COPY apps/reasoner/pyproject.toml ./
COPY apps/reasoner/reasoner ./reasoner
RUN pip install --no-cache-dir -e .
COPY packages/nodes/catalog /app/packages/nodes/catalog
COPY packages/nodes/pieces/index.json /app/packages/nodes/pieces/index.json
RUN useradd --system --uid 10001 agently
USER agently
CMD ["python", "-m", "reasoner.worker"]
