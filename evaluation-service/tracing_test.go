package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// setupTracing instala um TracerProvider em memória e o propagador W3C,
// como o initTracer faz em produção.
func setupTracing(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return rec
}

// upstream simula flag-service / targeting-service e guarda o traceparent recebido.
type upstream struct {
	mu          sync.Mutex
	traceparent string
	server      *httptest.Server
}

func newUpstream(t *testing.T, status int, body any) *upstream {
	u := &upstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.traceparent = r.Header.Get("Traceparent")
		u.mu.Unlock()
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *upstream) received() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.traceparent
}

func testApp(flagURL, targetingURL string) *App {
	return &App{
		HttpClient:          &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		FlagServiceURL:      flagURL,
		TargetingServiceURL: targetingURL,
	}
}

// As chamadas ao flag-service e ao targeting-service precisam levar o
// traceparent da requisição original: é isso que forma o trace distribuído.
func TestFetchFromServicesPropagaTrace(t *testing.T) {
	setupTracing(t)
	flag := newUpstream(t, http.StatusOK, Flag{Name: "nova-home", IsEnabled: true})
	rule := newUpstream(t, http.StatusOK, TargetingRule{FlagName: "nova-home", IsEnabled: true,
		Rules: Rule{Type: "PERCENTAGE", Value: 50}})
	a := testApp(flag.server.URL, rule.server.URL)

	ctx, span := otel.Tracer("test").Start(context.Background(), "GET /evaluate")
	info, err := a.fetchFromServices(ctx, "nova-home")
	span.End()

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if info.Flag == nil || !info.Flag.IsEnabled || info.Rule == nil {
		t.Fatalf("informações combinadas incompletas: %+v", info)
	}
	wantTrace := span.SpanContext().TraceID().String()
	for name, u := range map[string]*upstream{"flag-service": flag, "targeting-service": rule} {
		got := u.received()
		if got == "" {
			t.Fatalf("%s não recebeu traceparent", name)
		}
		if sc := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(),
			propagation.HeaderCarrier{"Traceparent": []string{got}})); sc.TraceID().String() != wantTrace {
			t.Fatalf("%s recebeu outro trace: %s (esperado %s)", name, sc.TraceID(), wantTrace)
		}
	}
}

func TestFetchFromServicesErros(t *testing.T) {
	setupTracing(t)
	ok := newUpstream(t, http.StatusOK, TargetingRule{FlagName: "x"})

	notFound := newUpstream(t, http.StatusNotFound, map[string]string{})
	_, err := testApp(notFound.server.URL, ok.server.URL).fetchFromServices(context.Background(), "x")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("flag inexistente deveria retornar NotFoundError, obteve %v", err)
	}

	broken := newUpstream(t, http.StatusInternalServerError, map[string]string{})
	if _, err := testApp(broken.server.URL, ok.server.URL).fetchFromServices(context.Background(), "x"); err == nil {
		t.Fatal("erro 500 do flag-service deveria ser propagado")
	}

	// Regra ausente não é fatal: usa o comportamento padrão
	flagOK := newUpstream(t, http.StatusOK, Flag{Name: "x", IsEnabled: true})
	ruleMissing := newUpstream(t, http.StatusNotFound, map[string]string{})
	info, err := testApp(flagOK.server.URL, ruleMissing.server.URL).fetchFromServices(context.Background(), "x")
	if err != nil || info.Rule != nil {
		t.Fatalf("regra ausente deveria ser tolerada: info=%+v err=%v", info, err)
	}

	ruleBroken := newUpstream(t, http.StatusBadGateway, map[string]string{})
	if _, err := testApp(flagOK.server.URL, ruleBroken.server.URL).fetchRule(context.Background(), "x"); err == nil {
		t.Fatal("erro 502 do targeting-service deveria ser retornado por fetchRule")
	}
}

// fakeSQS registra a mensagem enviada (sqsiface permite substituir o cliente real).
type fakeSQS struct {
	sqsiface.SQSAPI
	input *sqs.SendMessageInput
	err   error
}

func (f *fakeSQS) SendMessageWithContext(_ aws.Context, in *sqs.SendMessageInput, _ ...request.Option) (*sqs.SendMessageOutput, error) {
	f.input = in
	if f.err != nil {
		return nil, f.err
	}
	return &sqs.SendMessageOutput{MessageId: aws.String("msg-1")}, nil
}

func TestSendEvaluationEventPublicaComTrace(t *testing.T) {
	rec := setupTracing(t)
	fake := &fakeSQS{}
	a := &App{SqsSvc: fake, SqsQueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/EventQueue"}

	ctx, parent := otel.Tracer("test").Start(context.Background(), "GET /evaluate")
	a.sendEvaluationEvent(ctx, "user-1", "nova-home", true)
	parent.End()

	if fake.input == nil {
		t.Fatal("nenhuma mensagem enviada")
	}
	var ev EvaluationEvent
	if err := json.Unmarshal([]byte(*fake.input.MessageBody), &ev); err != nil || ev.FlagName != "nova-home" || !ev.Result {
		t.Fatalf("corpo da mensagem inválido: %s (%v)", *fake.input.MessageBody, err)
	}
	if _, ok := fake.input.MessageAttributes["traceparent"]; !ok {
		t.Fatalf("traceparent ausente dos MessageAttributes: %v", fake.input.MessageAttributes)
	}

	var producer sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.SpanKind() == trace.SpanKindProducer {
			producer = s
		}
	}
	if producer == nil {
		t.Fatal("span PRODUCER não foi criado")
	}
	if producer.Name() != "EventQueue publish" || producer.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("span PRODUCER inesperado: nome=%q pai=%s", producer.Name(), producer.Parent().SpanID())
	}
}

func TestSendEvaluationEventFalhaMarcaErro(t *testing.T) {
	rec := setupTracing(t)
	a := &App{SqsSvc: &fakeSQS{err: errors.New("sqs indisponível")}, SqsQueueURL: "https://sqs/123/EventQueue"}

	a.sendEvaluationEvent(context.Background(), "user-1", "f", false)

	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		t.Fatalf("falha no envio deveria marcar o span com erro: %+v", spans)
	}
}

func TestSendEvaluationEventSemFila(t *testing.T) {
	rec := setupTracing(t)
	(&App{}).sendEvaluationEvent(context.Background(), "user-1", "f", false)
	if n := len(rec.Ended()); n != 0 {
		t.Fatalf("sem fila configurada não deveria criar spans, criou %d", n)
	}
}
