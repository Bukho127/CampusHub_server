package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/Bukho127/CampusHub_server/internal/auth"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	if !h.service.Available() {
		h.writeError(w, ErrUnavailable)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/form-data" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be multipart/form-data"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+(64<<10))
	err = r.ParseMultipartForm(1 << 20)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		var maxError *http.MaxBytesError
		if errors.As(err, &maxError) {
			h.writeError(w, ErrTooLarge)
		} else {
			writeJSON(w, 400, map[string]string{"error": "invalid multipart upload"})
		}
		return
	}
	form := r.MultipartForm
	if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		writeJSON(w, 400, map[string]string{"error": "provide exactly one image in the file field; extra fields are not supported"})
		return
	}
	header := form.File["file"][0]
	if header.Size > MaxUploadBytes {
		h.writeError(w, ErrTooLarge)
		return
	}
	input, err := header.Open()
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer input.Close()
	account, _ := auth.AccountFromContext(r.Context())
	f, err := h.service.Upload(r.Context(), account, header.Filename, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/media/"+f.ID)
	writeJSON(w, http.StatusCreated, f)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	account, _ := auth.AccountFromContext(r.Context())
	f, err := h.service.Get(r.Context(), account, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := 20, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if values, ok := r.URL.Query()[key]; ok {
			if len(values) != 1 {
				writeJSON(w, 400, map[string]string{"error": key + " must be provided once"})
				return
			}
			value, err := strconv.Atoi(values[0])
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": key + " must be an integer"})
				return
			}
			*target = value
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 2147483647 {
		writeJSON(w, 400, map[string]string{"error": "limit must be 1 to 100 and offset must be 0 to 2147483647"})
		return
	}
	account, _ := auth.AccountFromContext(r.Context())
	page, err := h.service.List(r.Context(), account, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) content(original bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, _ := auth.AccountFromContext(r.Context())
		f, stream, err := h.service.Open(r.Context(), account, r.PathValue("id"), original)
		if err != nil {
			h.writeError(w, err)
			return
		}
		defer stream.Close()
		size := f.Size
		disposition := "inline"
		if original {
			size, disposition = f.OriginalSize, "attachment"
		}
		w.Header().Set("Content-Type", f.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.FileName}))
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Accept-Ranges", "none")
		if r.Method == http.MethodHead {
			return
		}
		if _, err := io.CopyN(w, stream, size); err != nil {
			h.logger.Error("stream media", "id", f.ID, "error", err)
		}
	}
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	account, _ := auth.AccountFromContext(r.Context())
	if err := h.service.Delete(r.Context(), account, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "internal server error"
	switch {
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrBusy):
		status, message = http.StatusServiceUnavailable, err.Error()
	case errors.Is(err, ErrNotFound):
		status, message = http.StatusNotFound, ErrNotFound.Error()
	case errors.Is(err, ErrTooLarge):
		status, message = http.StatusRequestEntityTooLarge, ErrTooLarge.Error()
	case errors.Is(err, ErrUnsupportedType):
		status, message = http.StatusUnsupportedMediaType, ErrUnsupportedType.Error()
	case errors.Is(err, ErrInvalidName), errors.Is(err, ErrInvalidImage):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, auth.ErrUnauthorized):
		status, message = http.StatusUnauthorized, auth.ErrUnauthorized.Error()
	case errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusGatewayTimeout, "media operation timed out"
	default:
		h.logger.Error("media operation failed", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
