import os
import json
from unittest.mock import patch

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import SpanKind, StatusCode

os.environ.setdefault("AWS_REGION", "us-east-1")
os.environ.setdefault(
    "AWS_SQS_URL", "https://sqs.us-east-1.amazonaws.com/123456789012/EventQueue"
)
os.environ.setdefault("AWS_DYNAMODB_TABLE", "ToggleMasterAnalytics")

exporter = InMemorySpanExporter()
provider = TracerProvider()
provider.add_span_processor(SimpleSpanProcessor(exporter))
trace.set_tracer_provider(provider)

with patch("boto3.Session"), patch("threading.Thread"):
    import app

TRACE_ID = "4bf92f3577b34da6a3ce929d0e0e4736"
PARENT_SPAN_ID = "00f067aa0ba902b7"


def _message(body, with_trace=True):
    message = {
        "MessageId": "msg-trace-1",
        "ReceiptHandle": "rh-1",
        "Body": body,
    }
    if with_trace:
        message["MessageAttributes"] = {
            "traceparent": {
                "DataType": "String",
                "StringValue": f"00-{TRACE_ID}-{PARENT_SPAN_ID}-01",
            }
        }
    return message


@patch("app.dynamodb_client")
@patch("app.sqs_client")
def test_consumer_continua_trace_do_evaluation(mock_sqs, mock_dynamo):
    """O span CONSUMER deve pertencer ao trace do evaluation-service."""
    exporter.clear()
    body = json.dumps({"user_id": "u1", "flag_name": "f1", "result": True})

    app.process_message(_message(body))

    spans = [s for s in exporter.get_finished_spans() if s.kind == SpanKind.CONSUMER]
    assert len(spans) == 1
    span = spans[0]
    assert format(span.context.trace_id, "032x") == TRACE_ID
    assert format(span.parent.span_id, "016x") == PARENT_SPAN_ID
    assert span.attributes["messaging.system"] == "aws_sqs"
    assert span.attributes["messaging.destination.name"] == "EventQueue"
    assert span.status.status_code != StatusCode.ERROR


@patch("app.dynamodb_client")
@patch("app.sqs_client")
def test_consumer_marca_erro_em_mensagem_invalida(mock_sqs, mock_dynamo):
    """Mensagem com JSON inválido deixa o span com status ERROR."""
    exporter.clear()

    app.process_message(_message("nao-e-json"))

    span = [s for s in exporter.get_finished_spans() if s.kind == SpanKind.CONSUMER][0]
    assert span.status.status_code == StatusCode.ERROR
    mock_sqs.delete_message.assert_not_called()


@patch("app.dynamodb_client")
@patch("app.sqs_client")
def test_consumer_sem_contexto_inicia_novo_trace(mock_sqs, mock_dynamo):
    """Sem traceparent (ex.: teste de carga do KEDA) o processamento segue normalmente."""
    exporter.clear()
    body = json.dumps({"user_id": "u2", "flag_name": "f2", "result": False})

    app.process_message(_message(body, with_trace=False))

    span = [s for s in exporter.get_finished_spans() if s.kind == SpanKind.CONSUMER][0]
    assert span.parent is None
    mock_dynamo.put_item.assert_called_once()
