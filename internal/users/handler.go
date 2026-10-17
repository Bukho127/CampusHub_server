package users

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
)

// Handler serves the user profile HTTP API.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeInput[Input](w, r)
	if !ok {
		return
	}
	profile, err := h.service.Create(r.Context(), input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/users/"+strconv.FormatInt(profile.ID, 10))
	writeJSON(w, http.StatusCreated, profile)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	profile, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := 20, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if values, exists := r.URL.Query()[key]; exists {
			if len(values) != 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": key + " must be provided once"})
				return
			}
			value, err := strconv.Atoi(values[0])
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": key + " must be an integer"})
				return
			}
			*target = value
		}
	}
	page, err := h.service.List(r.Context(), limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	input, ok := decodeInput[ProfileInput](w, r)
	if !ok {
		return
	}
	profile, err := h.service.Update(r.Context(), id, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validation.Message})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrNotFound.Error()})
	case errors.Is(err, ErrEmailExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": ErrEmailExists.Error()})
	case errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "request timed out"})
	default:
		h.logger.Error("user operation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
		return 0, false
	}
	return id, true
}

func decodeInput[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var zero T
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return zero, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input *T
	err = decoder.Decode(&input)
	if err == nil && input != nil {
		var extra any
		err = decoder.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return *input, true
		}
	}
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body exceeds 16 KiB"})
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be one JSON object containing only fields supported by this endpoint"})
	}
	return zero, false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
