package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"sync/atomic"
)

// Injeção de falha controlada (chaos engineering) para demonstrar o ciclo
// alerta -> incidente -> self-healing.
//
// O percentual de erro vive apenas em memória: quando o self-healing executa
// o rollout restart, os novos pods sobem com 0% e o serviço se recupera.
// O /health continua saudável de propósito, para que a cura venha da
// automação disparada pelo alerta e não da liveness probe do Kubernetes.
type chaos struct {
	errorRate atomic.Int64 // percentual de 0 a 100
}

type chaosRequest struct {
	ErrorRate *int64 `json:"error_rate"`
}

// handler: GET consulta o estado atual; POST define o percentual de erro.
// Protegido pelo masterKeyAuthMiddleware.
func (c *chaos) handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var req chaosRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ErrorRate == nil {
			http.Error(w, "Corpo inválido: informe {\"error_rate\": 0-100}", http.StatusBadRequest)
			return
		}
		if *req.ErrorRate < 0 || *req.ErrorRate > 100 {
			http.Error(w, "error_rate deve estar entre 0 e 100", http.StatusBadRequest)
			return
		}
		c.errorRate.Store(*req.ErrorRate)
		// Registra o valor já validado e armazenado (não o dado bruto da requisição)
		log.Printf("CHAOS: injeção de falha ajustada para %d%% das requisições em /validate", c.errorRate.Load())
	default:
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int64{"error_rate": c.errorRate.Load()}); err != nil {
		http.Error(w, "Erro ao gerar JSON", http.StatusInternalServerError)
	}
}

// middleware devolve HTTP 500 no percentual configurado de requisições.
func (c *chaos) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rate := c.errorRate.Load(); rate > 0 && rand.Int64N(100) < rate {
			// %q escapa caracteres de controle: impede forjar linhas de log pela URL
			log.Printf("ERROR: falha injetada (chaos %d%%) em %q %q", rate, r.Method, r.URL.Path)
			http.Error(w, "Erro interno (falha injetada)", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r)
	})
}
