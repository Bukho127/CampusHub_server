package media

import (
	"net/http"

	"github.com/Bukho127/CampusHub_server/internal/middleware"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux,
	authorization *middleware.Auth) {
	mux.Handle("POST /api/v1/media",
		authorization.Require(http.HandlerFunc(h.upload)))

	mux.Handle("GET /api/v1/media",
		authorization.Require(http.HandlerFunc(h.list)))

	mux.Handle("GET /api/v1/media/{id}",
		authorization.Require(http.HandlerFunc(h.get)))

	mux.Handle("GET /api/v1/media/{id}/content",
		authorization.Require(h.content(false)))

	mux.Handle("GET /api/v1/media/{id}/original",
		authorization.Require(h.content(true)))

	mux.Handle("DELETE /api/v1/media/{id}",
		authorization.Require(http.HandlerFunc(h.delete)))
}
