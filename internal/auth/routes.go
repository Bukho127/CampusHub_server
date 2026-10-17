package auth

import "net/http"

// RegisterRoutes keeps login public and requires authentication for logout.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, require func(http.Handler) http.Handler) {
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.Handle("POST /api/v1/auth/logout", require(http.HandlerFunc(h.logout)))
}
