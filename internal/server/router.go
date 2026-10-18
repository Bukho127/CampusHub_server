package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/auth"
	"github.com/Bukho127/CampusHub_server/internal/media"
	"github.com/Bukho127/CampusHub_server/internal/middleware"
	"github.com/Bukho127/CampusHub_server/internal/users"
)

// NewRouter wires application endpoints and bounds request processing time.
func NewRouter(service *users.Service, authentication *auth.Service, uploads *media.Service, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	authorization := middleware.NewAuth(authentication, logger)
	profiles := users.NewHandler(service, logger)
	profiles.RegisterRoutes(mux, authorization)
	auth.NewHandler(authentication, logger).RegisterRoutes(
		mux,
		authorization.Require,
		profiles.Register(authentication),
	)
	media.NewHandler(uploads, logger).RegisterRoutes(mux, authorization)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeout := 5 * time.Second
		if r.URL.Path == "/api/v1/media" || strings.HasPrefix(r.URL.Path, "/api/v1/media/") {
			timeout = 45 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
