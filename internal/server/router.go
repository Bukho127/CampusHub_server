package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/users"
)

// NewRouter wires application endpoints and bounds request processing time.
func NewRouter(service *users.Service, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	users.NewHandler(service, logger).RegisterRoutes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
