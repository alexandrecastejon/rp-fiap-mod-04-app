package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestChaosMiddlewareSemFalha(t *testing.T) {
	c := &chaos{}
	rec := httptest.NewRecorder()
	c.middleware(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/validate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("com 0%% de falha esperava 200, obteve %d", rec.Code)
	}
}

func TestChaosMiddlewareFalhaTotal(t *testing.T) {
	c := &chaos{}
	c.errorRate.Store(100)
	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		c.middleware(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/validate", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("com 100%% de falha esperava 500, obteve %d", rec.Code)
		}
	}
}

func TestChaosHandler(t *testing.T) {
	cases := []struct {
		name, method, body string
		want               int
		wantRate           int64
	}{
		{"consulta", http.MethodGet, "", http.StatusOK, 0},
		{"ativa 50%", http.MethodPost, `{"error_rate": 50}`, http.StatusOK, 50},
		{"acima de 100", http.MethodPost, `{"error_rate": 101}`, http.StatusBadRequest, 0},
		{"negativo", http.MethodPost, `{"error_rate": -1}`, http.StatusBadRequest, 0},
		{"sem campo", http.MethodPost, `{}`, http.StatusBadRequest, 0},
		{"metodo invalido", http.MethodDelete, "", http.StatusMethodNotAllowed, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &chaos{}
			rec := httptest.NewRecorder()
			c.handler(rec, httptest.NewRequest(tc.method, "/admin/chaos", strings.NewReader(tc.body)))
			if rec.Code != tc.want {
				t.Fatalf("status: esperava %d, obteve %d", tc.want, rec.Code)
			}
			if got := c.errorRate.Load(); got != tc.wantRate {
				t.Fatalf("error_rate: esperava %d, obteve %d", tc.wantRate, got)
			}
		})
	}
}

func TestChaosExigeMasterKey(t *testing.T) {
	app := &App{MasterKey: "chave-de-teste"}
	h := app.masterKeyAuthMiddleware(http.HandlerFunc((&chaos{}).handler))

	for _, auth := range []string{"", "Bearer errada"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/chaos", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("Authorization %q: esperava 403, obteve %d", auth, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/chaos", nil)
	req.Header.Set("Authorization", "Bearer chave-de-teste")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("com a master key correta esperava 200, obteve %d", rec.Code)
	}
}
