package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/InfraVex/api-monitor/internal/target"
)

// maxRequestBodyBytes caps how much of a request body we will read, so a
// client cannot exhaust server memory with an oversized payload.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// TargetHandler adapts HTTP requests to target.Service calls. It contains
// no business logic: parsing, status codes, and JSON encoding only.
type TargetHandler struct {
	service *target.Service
	logger  *slog.Logger
}

// NewTargetHandler creates a TargetHandler backed by service.
func NewTargetHandler(service *target.Service, logger *slog.Logger) *TargetHandler {
	return &TargetHandler{service: service, logger: logger}
}

// Create handles POST /api/v1/targets.
func (h *TargetHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var req targetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "request body must be valid JSON")
		return
	}

	params, err := req.toNewParams()
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	t, err := h.service.Create(r.Context(), params)
	if err != nil {
		h.writeServiceError(w, r, err, true)
		return
	}

	writeJSON(w, http.StatusCreated, toTargetResponse(t))
}

// List handles GET /api/v1/targets.
func (h *TargetHandler) List(w http.ResponseWriter, r *http.Request) {
	targets, err := h.service.List(r.Context())
	if err != nil {
		h.writeServiceError(w, r, err, false)
		return
	}

	resp := targetListResponse{Targets: make([]targetResponse, 0, len(targets))}
	for _, t := range targets {
		resp.Targets = append(resp.Targets, toTargetResponse(t))
	}
	writeJSON(w, http.StatusOK, resp)
}

// Get handles GET /api/v1/targets/{id}.
func (h *TargetHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	t, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, r, err, false)
		return
	}
	writeJSON(w, http.StatusOK, toTargetResponse(t))
}

// Update handles PUT /api/v1/targets/{id}.
func (h *TargetHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var req targetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "request body must be valid JSON")
		return
	}

	params, err := req.toUpdateParams()
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	t, err := h.service.Update(r.Context(), id, params)
	if err != nil {
		h.writeServiceError(w, r, err, true)
		return
	}
	writeJSON(w, http.StatusOK, toTargetResponse(t))
}

// Delete handles DELETE /api/v1/targets/{id}.
func (h *TargetHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.service.Delete(r.Context(), id); err != nil {
		h.writeServiceError(w, r, err, false)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeServiceError maps an error returned from target.Service to an HTTP
// response.
//
// target.ErrNotFound and target.ErrAlreadyExists are always recognized.
// When allowValidation is true (create/update, where the error can
// legitimately be a domain validation failure caused by the client's
// input), anything else is reported as 400 with the error's own message,
// since in this milestone the only other errors Create/Update can return
// are target.Validate failures. When allowValidation is false (read/list/
// delete, which take no validated input), an unrecognized error indicates
// something unexpected, so it is logged and reported as a generic 500
// without leaking internal detail to the client.
func (h *TargetHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, allowValidation bool) {
	switch {
	case errors.Is(err, target.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "target not found")
	case errors.Is(err, target.ErrAlreadyExists):
		writeError(w, http.StatusConflict, codeInvalidRequest, "target already exists")
	case allowValidation:
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
	default:
		h.logger.Error("unexpected target service error", "error", err, "request_id", requestIDFromContext(r.Context()))
		writeError(w, http.StatusInternalServerError, codeInternalError, "internal error")
	}
}
