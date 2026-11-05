package localapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

const (
	browserSessionCookie = "RayanLampSession"
	browserSessionTTL    = 8 * time.Hour
	maxBrowserSessions   = 64
)

type browserSession struct {
	csrfToken string
	csrfHash  [sha256.Size]byte
	expires   time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]browserSession
	now      func() time.Time
	closed   bool
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		sessions: make(map[[sha256.Size]byte]browserSession),
		now:      time.Now,
	}
}

func (s *sessionStore) issue() (cookieValue, csrfToken string, expires time.Time, err error) {
	if s == nil {
		return "", "", time.Time{}, errors.New("localapi: browser sessions are unavailable")
	}
	cookieValue, err = randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	csrfToken, err = randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	cookieHash := sha256.Sum256([]byte(cookieValue))
	expires = s.now().UTC().Add(browserSessionTTL)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", "", time.Time{}, errors.New("localapi: browser sessions are closed")
	}
	s.pruneLocked(s.now())
	if len(s.sessions) >= maxBrowserSessions {
		var oldestKey [sha256.Size]byte
		var oldest time.Time
		for key, session := range s.sessions {
			if oldest.IsZero() || session.expires.Before(oldest) {
				oldestKey = key
				oldest = session.expires
			}
		}
		delete(s.sessions, oldestKey)
	}
	s.sessions[cookieHash] = browserSession{
		csrfToken: csrfToken,
		csrfHash:  sha256.Sum256([]byte(csrfToken)),
		expires:   expires,
	}
	return cookieValue, csrfToken, expires, nil
}

func (s *sessionStore) authenticate(r *http.Request) (browserSession, bool) {
	if s == nil || r == nil {
		return browserSession{}, false
	}
	cookies := r.Cookies()
	var value string
	count := 0
	for _, cookie := range cookies {
		if cookie.Name == browserSessionCookie {
			count++
			value = cookie.Value
		}
	}
	if count != 1 || !validRandomToken(value) {
		return browserSession{}, false
	}
	key := sha256.Sum256([]byte(value))
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return browserSession{}, false
	}
	session, ok := s.sessions[key]
	if !ok {
		return browserSession{}, false
	}
	if !now.Before(session.expires) {
		delete(s.sessions, key)
		return browserSession{}, false
	}
	return session, true
}

func (s *sessionStore) validateCSRF(session browserSession, candidate string) bool {
	if !validRandomToken(candidate) {
		return false
	}
	candidateHash := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(session.csrfHash[:], candidateHash[:]) == 1
}

func (s *sessionStore) pruneLocked(now time.Time) {
	for key, session := range s.sessions {
		if !now.Before(session.expires) {
			delete(s.sessions, key)
		}
	}
}

func (s *sessionStore) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	clear(s.sessions)
	s.mu.Unlock()
}

func browserCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     browserSessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// Deliberately omit Domain so this is a host-only cookie. Secure cannot
		// be used on the intentionally HTTP-only loopback listener.
	}
}

func randomToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func validRandomToken(value string) bool {
	if len(value) != base64.RawURLEncoding.EncodedLen(32) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}
