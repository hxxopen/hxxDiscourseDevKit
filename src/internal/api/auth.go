package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
)

const (
	sessionCookieName = "hxxddk_session"
	sessionTTL        = 8 * time.Hour
)

type session struct {
	ID        string
	CSRF      string
	CreatedAt time.Time
	ExpiresAt time.Time
	LastSeen  time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]*session{}}
}

// getOrCreate returns the current browser session, creating one if needed and
// refreshing the cookie.
func (s *sessionStore) getOrCreate(w http.ResponseWriter, r *http.Request) session {
	now := time.Now()
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		s.mu.Lock()
		if existing, ok := s.sessions[cookie.Value]; ok && now.Before(existing.ExpiresAt) {
			existing.LastSeen = now
			view := *existing
			s.mu.Unlock()
			s.setCookie(w, view)
			return view
		}
		s.mu.Unlock()
	}

	id, _ := secret.RandomHex(32)
	csrf, _ := secret.RandomHex(24)
	view := session{
		ID:        id,
		CSRF:      csrf,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
		LastSeen:  now,
	}

	s.mu.Lock()
	s.cleanupLocked(now)
	s.sessions[id] = &view
	s.mu.Unlock()

	s.setCookie(w, view)
	return view
}

func (s *sessionStore) get(id string) (string, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.sessions[id]
	if !ok || now.After(existing.ExpiresAt) {
		if ok {
			delete(s.sessions, id)
		}
		return "", false
	}
	existing.LastSeen = now
	return existing.CSRF, true
}

func (s *sessionStore) cleanupLocked(now time.Time) {
	for id, existing := range s.sessions {
		if now.After(existing.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func (s *sessionStore) setCookie(w http.ResponseWriter, view session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    view.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // loopback HTTP only
		SameSite: http.SameSiteStrictMode,
	})
}

// auth accepts either the CLI bearer token or a browser session. Mutating
// requests authenticated by cookie require a matching CSRF header, and any
// mutating request carrying an Origin must come from an allowed origin.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := bearerToken(r); token != "" {
			if !constantTimeEqual(token, s.opts.Secrets.APIToken()) {
				httpx.WriteError(w, r, httpx.Unauthorized("invalid API token"), s.log)
				return
			}
			if isMutating(r.Method) && !s.originAllowed(r) {
				httpx.WriteError(w, r,
					httpx.Forbidden("FORBIDDEN_ORIGIN", "request origin is not allowed"), s.log)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if csrf, ok := s.sessions.get(cookie.Value); ok {
				if isMutating(r.Method) {
					if !s.originAllowed(r) {
						httpx.WriteError(w, r,
							httpx.Forbidden("FORBIDDEN_ORIGIN", "request origin is not allowed"), s.log)
						return
					}
					provided := r.Header.Get("X-CSRF-Token")
					if provided == "" || !constantTimeEqual(provided, csrf) {
						httpx.WriteError(w, r,
							httpx.Forbidden("CSRF_INVALID", "missing or invalid CSRF token"), s.log)
						return
					}
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		httpx.WriteError(w, r,
			httpx.Unauthorized("authentication required"), s.log)
	})
}

func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	origin = strings.ToLower(strings.TrimRight(strings.TrimSpace(origin), "/"))
	return s.allowedOrigins[origin]
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}
