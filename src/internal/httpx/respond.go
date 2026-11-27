package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// ErrorBody is the stable error envelope returned by the API.
type ErrorBody struct {
	Error struct {
		Code      string         `json:"code"`
		Message   string         `json:"message"`
		Details   map[string]any `json:"details,omitempty"`
		RequestID string         `json:"request_id,omitempty"`
	} `json:"error"`
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// WriteError writes an API error envelope. Unknown errors are masked as 500.
func WriteError(w http.ResponseWriter, r *http.Request, err error, logger *slog.Logger) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}
	requestID := RequestIDFrom(r.Context())
	if apiErr.Status >= http.StatusInternalServerError {
		logger.Error("request failed",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", apiErr.Status,
			"error", err,
		)
	} else {
		logger.Debug("request rejected",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", apiErr.Status,
			"code", apiErr.Code,
		)
	}

	var body ErrorBody
	body.Error.Code = apiErr.Code
	body.Error.Message = apiErr.Message
	body.Error.Details = apiErr.Details
	body.Error.RequestID = requestID
	WriteJSON(w, apiErr.Status, body)
}

// DecodeJSON decodes a size-limited JSON request body, rejecting unknown
// fields and trailing data.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return BadRequest("VALIDATION_FAILED", "请求体无效: "+err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return BadRequest("VALIDATION_FAILED", "请求体只能包含一个 JSON 对象")
	}
	return nil
}
