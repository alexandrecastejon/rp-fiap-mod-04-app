import os
import sys
import threading
import json
import uuid
import time
import logging
import boto3
from botocore.exceptions import NoCredentialsError, ClientError
from flask import Flask, jsonify
from dotenv import load_dotenv
from datetime import datetime, timezone
from opentelemetry import propagate, trace
from opentelemetry.trace import SpanKind, Status, StatusCode

# Configura o logging
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s"
)
log = logging.getLogger(__name__)

# Carrega .env para desenvolvimento local
load_dotenv()

# --- Configuração ---
AWS_REGION = os.getenv("AWS_REGION")
SQS_QUEUE_URL = os.getenv("AWS_SQS_URL")
DYNAMODB_TABLE_NAME = os.getenv("AWS_DYNAMODB_TABLE")

if not all([AWS_REGION, SQS_QUEUE_URL, DYNAMODB_TABLE_NAME]):
    log.critical(
        "Erro: AWS_REGION, AWS_SQS_URL, e AWS_DYNAMODB_TABLE devem ser definidos."
    )
    sys.exit(1)

# Nome da fila (último segmento da URL), usado nos spans de mensageria
QUEUE_NAME = SQS_QUEUE_URL.rstrip("/").rsplit("/", 1)[-1]
tracer = trace.get_tracer("analytics-service/sqs")

# --- Clientes Boto3 ---
# Criamos a sessão uma vez
try:
    session = boto3.Session(region_name=AWS_REGION)
    sqs_client = session.client("sqs")
    dynamodb_client = session.client("dynamodb")
    log.info(f"Clientes Boto3 inicializados na região {AWS_REGION}")
except NoCredentialsError:
    log.critical("Credenciais da AWS não encontradas. Verifique seu ambiente.")
    sys.exit(1)
except Exception as e:
    log.critical(f"Erro ao inicializar o Boto3: {e}")
    sys.exit(1)


# --- SQS Worker ---


def _trace_carrier(message):
    """Extrai dos MessageAttributes o contexto de trace (traceparent) enviado
    pelo evaluation-service, no formato esperado pelo propagador do OTel."""
    attributes = message.get("MessageAttributes") or {}
    return {
        key: value.get("StringValue", "")
        for key, value in attributes.items()
        if value.get("DataType") == "String"
    }


def process_message(message):
    """Processa uma mensagem SQS continuando o trace iniciado no evaluation-service.

    O span CONSUMER tem como pai o span PRODUCER que publicou a mensagem, o que
    conecta evaluation -> SQS -> analytics -> DynamoDB no mesmo trace distribuído.
    """
    parent = propagate.extract(_trace_carrier(message))
    with tracer.start_as_current_span(
        f"{QUEUE_NAME} process",
        context=parent,
        kind=SpanKind.CONSUMER,
        attributes={
            "messaging.system": "aws_sqs",
            "messaging.operation.type": "process",
            "messaging.destination.name": QUEUE_NAME,
            "messaging.message.id": message.get("MessageId", ""),
        },
    ) as span:
        _process_message(message, span)


def _process_message(message, span):
    """Processa uma única mensagem SQS e a insere no DynamoDB"""
    try:
        log.info(f"Processando mensagem ID: {message['MessageId']}")
        body = json.loads(message["Body"])

        # Gera um ID único para o item no DynamoDB
        event_id = str(uuid.uuid4())

        # Constrói o item no formato do DynamoDB
        item = {
            "event_id": {"S": event_id},
            "user_id": {"S": body.get("user_id", "unknown-user")},
            "flag_name": {"S": body.get("flag_name", "unknown-flag")},
            "result": {"BOOL": body.get("result", False)},
            "timestamp": {"S": body.get("timestamp", datetime.now(timezone.utc).isoformat())},
        }

        # Insere no DynamoDB
        dynamodb_client.put_item(TableName=DYNAMODB_TABLE_NAME, Item=item)

        log.info(f"Evento {event_id} (Flag: {body['flag_name']}) salvo no DynamoDB.")

        # Se tudo deu certo, deleta a mensagem da fila
        sqs_client.delete_message(
            QueueUrl=SQS_QUEUE_URL, ReceiptHandle=message["ReceiptHandle"]
        )

    except json.JSONDecodeError as e:
        log.error(f"Erro ao decodificar JSON da mensagem ID: {message['MessageId']}")
        span.record_exception(e)
        span.set_status(Status(StatusCode.ERROR, "JSON inválido na mensagem"))
        # Não deleta a mensagem, pode ser uma "poison pill"
    except ClientError as e:
        log.error(
            f"Erro do Boto3 (DynamoDB ou SQS) ao processar {message['MessageId']}: {e}"
        )
        span.record_exception(e)
        span.set_status(Status(StatusCode.ERROR, "erro do DynamoDB ou SQS"))
        # Não deleta a mensagem, tenta novamente
    except Exception as e:
        log.error(f"Erro inesperado ao processar {message['MessageId']}: {e}")
        span.record_exception(e)
        span.set_status(Status(StatusCode.ERROR, "erro inesperado"))
        # Não deleta a mensagem, tenta novamente


def sqs_worker_loop():
    """Loop principal do worker que ouve a fila SQS"""
    log.info("Iniciando o worker SQS...")
    while True:
        try:
            # Long-polling: espera até 20s por mensagens
            response = sqs_client.receive_message(
                QueueUrl=SQS_QUEUE_URL,
                MaxNumberOfMessages=10,  # Processa em lotes de até 10
                WaitTimeSeconds=20,
                # Necessário para receber o traceparent propagado pelo evaluation-service
                MessageAttributeNames=["All"],
            )

            messages = response.get("Messages", [])
            if not messages:
                # Nenhuma mensagem, continua o loop
                continue

            log.info(f"Recebidas {len(messages)} mensagens.")

            for message in messages:
                process_message(message)

        except ClientError as e:
            log.error(f"Erro do Boto3 no loop principal do SQS: {e}")
            time.sleep(10)  # Pausa antes de tentar novamente
        except Exception as e:
            log.error(f"Erro inesperado no loop principal do SQS: {e}")
            time.sleep(10)


# --- Servidor Flask (Apenas para Health Check) ---

app = Flask(__name__)


@app.route("/health")
def health():
    # Uma verificação de saúde real poderia checar a conexão com o DynamoDB/SQS
    return jsonify({"status": "ok"})


# --- Inicialização ---


def start_worker():
    """Inicia o worker SQS em uma thread separada"""
    worker_thread = threading.Thread(target=sqs_worker_loop, daemon=True)
    worker_thread.start()


# Inicia o worker SQS em uma thread de background
# Isso garante que ele inicie tanto com 'flask run' quanto com 'gunicorn'
start_worker()

if __name__ == "__main__":
    port = int(os.getenv("PORT", 8005))
    app.run(host="0.0.0.0", port=port, debug=False)
