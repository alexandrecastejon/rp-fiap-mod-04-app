package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/go-redis/redis/v8"
)

// syncSQS registra as mensagens publicadas de forma segura (o handler publica em goroutine).
type syncSQS struct {
	sqsiface.SQSAPI
	mu   sync.Mutex
	sent []*sqs.SendMessageInput
}

func (f *syncSQS) SendMessageWithContext(_ aws.Context, in *sqs.SendMessageInput, _ ...request.Option) (*sqs.SendMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, in)
	return &sqs.SendMessageOutput{MessageId: aws.String("msg")}, nil
}

func (f *syncSQS) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// Fluxo completo do /evaluate: cache miss -> flag/targeting -> cache -> evento SQS;
// a segunda chamada é atendida pelo cache, sem chamar os serviços de novo.
func TestEvaluationHandlerPontaAPonta(t *testing.T) {
	setupTracing(t)
	mr := miniredis.RunT(t)

	flagCalls := 0
	var mu sync.Mutex
	flagSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		flagCalls++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(Flag{Name: "nova-home", IsEnabled: true})
	}))
	t.Cleanup(flagSrv.Close)
	ruleSrv := newUpstream(t, http.StatusNotFound, map[string]string{})

	fake := &syncSQS{}
	a := testApp(flagSrv.URL, ruleSrv.server.URL)
	a.RedisClient = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	a.SqsSvc = fake
	a.SqsQueueURL = "https://sqs.us-east-1.amazonaws.com/123456789012/EventQueue"

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/evaluate?user_id=user-1&flag_name=nova-home", nil)
		a.evaluationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("chamada %d: esperava 200, obteve %d (%s)", i+1, rec.Code, rec.Body.String())
		}
		var resp EvaluationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.FlagName != "nova-home" {
			t.Fatalf("resposta inválida: %s", rec.Body.String())
		}
	}

	if flagCalls != 1 {
		t.Fatalf("a segunda avaliação deveria vir do cache: flag-service chamado %d vezes", flagCalls)
	}
	if !mr.Exists("flag_info:nova-home") {
		t.Fatal("a informação combinada deveria ter sido gravada no Redis")
	}

	// O evento é publicado de forma assíncrona
	deadline := time.Now().Add(2 * time.Second)
	for fake.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := fake.count(); n != 2 {
		t.Fatalf("esperava 2 eventos publicados na SQS, obteve %d", n)
	}
}

func TestEvaluationHandlerErros(t *testing.T) {
	setupTracing(t)
	mr := miniredis.RunT(t)

	a := testApp("http://127.0.0.1:1", "http://127.0.0.1:1") // serviços indisponíveis
	a.RedisClient = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	rec := httptest.NewRecorder()
	a.evaluationHandler(rec, httptest.NewRequest(http.MethodGet, "/evaluate?user_id=u", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sem flag_name esperava 400, obteve %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	a.evaluationHandler(rec, httptest.NewRequest(http.MethodGet, "/evaluate?user_id=u&flag_name=f", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("com flag-service fora do ar esperava 502, obteve %d", rec.Code)
	}
}
