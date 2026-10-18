FROM python:3.11-slim@sha256:e88e9763f943ec1834f992a4b51e0f24500486803e8bc534e5767af9ea65f6ce AS builder
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends gcc build-essential && rm -rf /var/lib/apt/lists/*
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY requirements.txt .
# Dependências com versões fixadas; o bootstrap instala as instrumentações compatíveis com o distro fixado
RUN pip install --no-cache-dir -r requirements.txt && opentelemetry-bootstrap -a install

FROM python:3.11-slim@sha256:e88e9763f943ec1834f992a4b51e0f24500486803e8bc534e5767af9ea65f6ce
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
