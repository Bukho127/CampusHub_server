package auth

import "net/http"

// RegisterRoutes keeps registration and login public and protects logout.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, require func(http.Handler) http.Handler, register http.Handler) {
	mux.Handle("POST /api/v1/auth/register", register)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.Handle("POST /api/v1/auth/logout", require(http.HandlerFunc(h.logout)))
}
