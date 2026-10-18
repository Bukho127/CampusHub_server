package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Bukho127/CampusHub_server/internal/auth"
)

type Auth struct {
	service *auth.Service
	logger  *slog.Logger
}

func NewAuth(service *auth.Service, logger *slog.Logger) *Auth {
	return &Auth{
		service: service,
		logger:  logger,
	}
}

func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			unauthorized(w)
			return
		}
		fields := strings.Fields(values[0])
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			unauthorized(w)
			return
		}
		account, err := a.service.Authenticate(r.Context(), fields[1])
		if errors.Is(err, auth.ErrUnauthorized) {
			unauthorized(w)
			return
		}
		if err != nil {
			a.logger.Error("authenticate request", "error", err)
			reply(w, http.StatusServiceUnavailable, "authentication temporarily unavailable")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithAccount(r.Context(), account)))
	})
}

func (a *Auth) Admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := auth.AccountFromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		if account.Role != "admin" {
			reply(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) OwnerOrAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := auth.AccountFromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			reply(w, http.StatusBadRequest, "id must be a positive integer")
			return
		}
		if account.ID != id && account.Role != "admin" {
			reply(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	reply(w, http.StatusUnauthorized, "invalid or expired access token")
}

func reply(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
