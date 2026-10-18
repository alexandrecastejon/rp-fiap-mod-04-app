package main

import "net/http"

// routes monta as rotas da API. Fica separado do main para que as regras de
// acesso (o que é público e o que exige a MASTER_KEY) sejam testáveis.
func (a *App) routes(faults *chaos) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.healthHandler)

	// Endpoint público para validar uma chave; passa pela injeção de falha controlada
	mux.Handle("/validate", faults.middleware(http.HandlerFunc(a.validateKeyHandler)))

	// Endpoints de "admin" para criar/gerenciar chaves e injetar falhas
	// Eles são protegidos pelo middleware de autenticação
	mux.Handle("/admin/keys", a.masterKeyAuthMiddleware(http.HandlerFunc(a.createKeyHandler)))
	mux.Handle("/admin/chaos", a.masterKeyAuthMiddleware(http.HandlerFunc(faults.handler)))

	return mux
}
