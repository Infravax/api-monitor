package api

import (
	"encoding/json"
	"net/http"
)

// errorCode is a small, stable set of machine-readable error categories.
// Clients should switch on this, not on the human-readable message.
type errorCode string

const (
	codeInvalidRequest errorCode = "INVALID_REQUEST"
	codeNotFound       errorCode = "NOT_FOUND"
	codeInternalError  errorCode = "INTERNAL_ERROR"
)

type errorBody struct {
	Code    errorCode `json:"code"`
	Message string    `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// writeJSON encodes v as the JSON response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a consistent JSON error envelope. message is sent to
// the client, so it must never contain internal implementation details;
// see writeInternalError for the case where it might.
func writeError(w http.ResponseWriter, status int, code errorCode, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}
