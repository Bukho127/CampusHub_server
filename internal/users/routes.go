package users

import (
	"net/http"

	"github.com/Bukho127/CampusHub_server/internal/middleware"
)

// RegisterRoutes adds the user profile endpoints to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authorization *middleware.Auth) {
	mux.HandleFunc("POST /api/v1/users", h.Create)
	mux.Handle("GET /api/v1/users", authorization.Require(authorization.Admin(http.HandlerFunc(h.list))))
	mux.Handle("GET /api/v1/users/{id}", authorization.Require(authorization.OwnerOrAdmin(http.HandlerFunc(h.get))))
	mux.Handle("PUT /api/v1/users/{id}", authorization.Require(authorization.OwnerOrAdmin(http.HandlerFunc(h.update))))
	mux.Handle("DELETE /api/v1/users/{id}", authorization.Require(authorization.OwnerOrAdmin(http.HandlerFunc(h.delete))))
}
