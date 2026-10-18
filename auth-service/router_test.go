package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(h http.Handler, method, path, auth, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRoutesRegrasDeAcesso(t *testing.T) {
	app := &App{MasterKey: "master-de-teste"}
	faults := &chaos{}
	h := app.routes(faults)

	if code := serve(h, http.MethodGet, "/health", "", ""); code != http.StatusOK {
		t.Fatalf("/health deveria ser público: %d", code)
	}
	for _, path := range []string{"/admin/keys", "/admin/chaos"} {
		if code := serve(h, http.MethodPost, path, "", "{}"); code != http.StatusForbidden {
			t.Fatalf("%s sem master key deveria retornar 403, retornou %d", path, code)
		}
		if code := serve(h, http.MethodPost, path, "chave-errada", "{}"); code != http.StatusForbidden {
			t.Fatalf("%s com chave errada deveria retornar 403, retornou %d", path, code)
		}
	}
}

// O ciclo da demonstração: o admin ativa a falha e o /validate passa a
// responder 500 (o que dispara o alerta); um pod novo (restart) volta a 0%.
func TestRoutesCicloDeFalhaEAutocura(t *testing.T) {
	app := &App{MasterKey: "master-de-teste"}
	h := app.routes(&chaos{})

	if code := serve(h, http.MethodPost, "/admin/chaos", "master-de-teste", `{"error_rate": 100}`); code != http.StatusOK {
		t.Fatalf("ativar a falha com a master key deveria funcionar: %d", code)
	}
	if code := serve(h, http.MethodGet, "/validate", "qualquer", ""); code != http.StatusInternalServerError {
		t.Fatalf("com 100%% de falha o /validate deveria retornar 500, retornou %d", code)
	}

	restarted := app.routes(&chaos{})
	if code := serve(restarted, http.MethodGet, "/admin/chaos", "master-de-teste", ""); code != http.StatusOK {
		t.Fatalf("consulta do estado após restart falhou: %d", code)
	}
	if code := serve(restarted, http.MethodGet, "/validate", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("após o restart o /validate deveria voltar ao normal (401 sem chave), retornou %d", code)
	}
}
