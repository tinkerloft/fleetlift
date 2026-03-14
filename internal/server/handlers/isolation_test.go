package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/auth"
)

func TestTeamIDFromRequest_RejectsCrossTeamAccess(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-A": "member"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Team-ID", "team-B")
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Empty(t, teamID)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTeamIDFromRequest_AllowsOwnTeam(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-A": "member"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Team-ID", "team-A")
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Equal(t, "team-A", teamID)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTeamIDFromRequest_SingleTeamFallback(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-only": "admin"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Equal(t, "team-only", teamID)
}

func TestTeamIDFromRequest_MultiTeamRequiresHeader(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		TeamRoles: map[string]string{"team-A": "member", "team-B": "admin"},
	}
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	teamID := teamIDFromRequest(w, req, claims)
	assert.Empty(t, teamID)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
