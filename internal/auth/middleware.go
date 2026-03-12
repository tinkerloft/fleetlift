package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			sseTicketMu.Lock()
			for k, t := range sseTickets {
				if now.After(t.expires) {
					delete(sseTickets, k)
				}
			}
			sseTicketMu.Unlock()
		}
	}()
}

var (
	sseTicketMu sync.Mutex
	sseTickets  = map[string]sseTicket{}
)

type sseTicket struct {
	claims     *Claims
	resourceID string // run ID or step run ID the ticket is valid for
	expires    time.Time
}

// IssueSSETicket creates a short-lived ticket that can be exchanged for a session in SSE endpoints.
// The ticket is bound to resourceID (a run ID or step run ID) and will be rejected if presented
// for a different resource.
func IssueSSETicket(claims *Claims, resourceID string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	ticket := hex.EncodeToString(b)
	sseTicketMu.Lock()
	sseTickets[ticket] = sseTicket{claims: claims, resourceID: resourceID, expires: time.Now().Add(60 * time.Second)}
	sseTicketMu.Unlock()
	return ticket
}

// ConsumeSSETicket validates and removes a ticket, returning its claims.
// resourceID must match what was provided to IssueSSETicket; a mismatch returns false.
func ConsumeSSETicket(ticket, resourceID string) (*Claims, bool) {
	sseTicketMu.Lock()
	defer sseTicketMu.Unlock()
	t, ok := sseTickets[ticket]
	if !ok || time.Now().After(t.expires) {
		delete(sseTickets, ticket)
		return nil, false
	}
	if t.resourceID != resourceID {
		// Do not consume — mismatched resource; leave ticket in place to avoid timing side-channels.
		return nil, false
	}
	delete(sseTickets, ticket)
	return t.claims, true
}

type contextKey string

const claimsKey contextKey = "claims"

// Middleware returns an HTTP middleware that validates JWT tokens from the
// Authorization header (Bearer) or fl_token cookie.
func Middleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			claims, err := ValidateToken(secret, token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts the JWT claims from the request context.
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	// Only accept cookie auth for safe (read-only) methods to prevent CSRF
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		if c, err := r.Cookie("fl_token"); err == nil {
			return c.Value
		}
	}
	return ""
}
