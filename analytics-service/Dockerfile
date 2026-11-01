FROM python:3.15-slim@sha256:94d3426f1def7db4a106a232ac121bfd975393e5dfd40a619b955fae45fe7913 AS builder
WORKDIR /app
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY requirements.txt .
# Lockfile com hashes: toda dependência (inclusive transitiva) tem versão e hash
# verificados; só pacotes binários (wheels), sem executar setup.py de terceiros
RUN pip install --no-cache-dir --only-binary :all: --require-hashes -r requirements.txt

FROM python:3.15-slim@sha256:94d3426f1def7db4a106a232ac121bfd975393e5dfd40a619b955fae45fe7913
ENV PATH="/opt/venv/bin:$PATH" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY --from=builder /opt/venv /opt/venv
# UID numérico: permite ao Kubernetes validar runAsNonRoot. O código pertence ao root
# e é somente leitura para a aplicação.
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin appuser
COPY . .
USER 10001
EXPOSE 8005
CMD ["opentelemetry-instrument", "python", "app.py"]
