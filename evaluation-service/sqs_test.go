package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// O contexto injetado nos MessageAttributes precisa ser extraído com o mesmo
// trace ID: é isso que conecta evaluation -> SQS -> analytics no Service Map.
func TestSqsAttributesCarrierPropagaTrace(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)

	prop := propagation.TraceContext{}
	attrs := sqsAttributesCarrier{}
	prop.Inject(ctx, attrs)

	v, ok := attrs["traceparent"]
	if !ok || v.DataType == nil || *v.DataType != "String" {
		t.Fatalf("traceparent ausente ou com DataType inválido: %#v", attrs)
	}

	got := trace.SpanContextFromContext(prop.Extract(context.Background(), attrs))
	if got.TraceID() != traceID || got.SpanID() != spanID || !got.IsSampled() {
		t.Fatalf("contexto extraído diferente: trace=%s span=%s", got.TraceID(), got.SpanID())
	}
}

func TestSqsAttributesCarrierChaveAusente(t *testing.T) {
	if v := (sqsAttributesCarrier{}).Get("traceparent"); v != "" {
		t.Fatalf("esperava string vazia, obteve %q", v)
	}
}
