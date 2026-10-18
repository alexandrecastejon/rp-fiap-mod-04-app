FROM python:3.14-slim@sha256:a2b82f3c48559aa0a8446d9af49826b6e2b2016f4cd2afabfe6013ec53729170 AS builder
WORKDIR /app
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY requirements.txt .
# Lockfile com hashes: toda dependência (inclusive transitiva) tem versão e hash
# verificados; só pacotes binários (wheels), sem executar setup.py de terceiros
RUN pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements.txt

FROM python:3.14-slim@sha256:a2b82f3c48559aa0a8446d9af49826b6e2b2016f4cd2afabfe6013ec53729170
ENV PATH="/opt/venv/bin:$PATH" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY --from=builder /opt/venv /opt/venv
# UID numérico: permite ao Kubernetes validar runAsNonRoot. O código pertence ao root
# e é somente leitura para a aplicação.
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin appuser
COPY . .
USER 10001
EXPOSE 8002
CMD ["opentelemetry-instrument", "python", "app.py"]
