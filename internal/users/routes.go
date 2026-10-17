package users

import "net/http"

// RegisterRoutes adds the user profile endpoints to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users", h.create)
	mux.HandleFunc("GET /api/v1/users", h.list)
	mux.HandleFunc("GET /api/v1/users/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/users/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.delete)
}
