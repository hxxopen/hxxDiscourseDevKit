package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type contextKey int

const requestIDKey contextKey = iota

// RequestIDFrom returns the request ID stored in ctx.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestID attaches a request ID to every request and response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 128 {
			var buf [8]byte
			if _, err := rand.Read(buf[:]); err == nil {
				id = "req_" + hex.EncodeToString(buf[:])
			} else {
				id = "req_unknown"
			}
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Recorder captures the response status and size while preserving Flusher.
type Recorder struct {
	http.ResponseWriter
	Status int
	Bytes  int64
}

// WriteHeader records the first status code written.
func (r *Recorder) WriteHeader(status int) {
	if r.Status != 0 {
		return
	}
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write records bytes written.
func (r *Recorder) Write(p []byte) (int, error) {
	if r.Status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(p)
	r.Bytes += int64(n)
	return n, err
}

// Flush implements http.Flusher when the underlying writer supports it.
func (r *Recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap allows http.ResponseController to reach the underlying writer.
func (r *Recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Recoverer converts panics into 500 responses.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder, ok := w.(*Recorder)
			if !ok {
				recorder = &Recorder{ResponseWriter: w}
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered",
						"request_id", RequestIDFrom(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"panic", recovered,
						"stack", string(debug.Stack()),
					)
					if recorder.Status == 0 {
						WriteError(recorder, r, Internal(nil), logger)
					}
				}
			}()
			next.ServeHTTP(recorder, r)
		})
	}
}

// SecurityHeaders adds browser hardening headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; font-src 'self'; connect-src 'self'; "+
				"object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// AccessLog logs one line per completed request.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder, ok := w.(*Recorder)
			if !ok {
				recorder = &Recorder{ResponseWriter: w}
			}
			next.ServeHTTP(recorder, r)
			status := recorder.Status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("http request",
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", recorder.Bytes,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// Chain applies middleware in the order listed: the first is outermost.
func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}
