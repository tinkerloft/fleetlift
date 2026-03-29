package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

func TestListSavedRepos_Unauthorized(t *testing.T) {
	h := &SavedRepoHandlers{}
	req := httptest.NewRequest("GET", "/api/saved-repos", nil)
	w := httptest.NewRecorder()
	h.ListSavedRepos(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateSavedRepo_Validation(t *testing.T) {
	h := &SavedRepoHandlers{}

	longLabel := strings.Repeat("a", 201)
	longURL := "https://github.com/org/" + strings.Repeat("a", 2048)
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"empty url", `{"url":""}`, "url is required"},
		{"missing url", `{}`, "url is required"},
		{"non-https url", `{"url":"git://github.com/org/repo"}`, "must use https://"},
		{"file url", `{"url":"file:///etc/passwd"}`, "must use https://"},
		{"https no host", `{"url":"https://"}`, "must use https://"},
		{"url too long", `{"url":"` + longURL + `"}`, "2048 characters"},
		{"label too long", `{"url":"https://github.com/org/repo","label":"` + longLabel + `"}`, "label must be 200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/saved-repos", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = claimsCtx(req, "")

			w := httptest.NewRecorder()
			h.CreateSavedRepo(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantMsg)
		})
	}
}

func TestDeleteSavedRepo_Unauthorized(t *testing.T) {
	h := &SavedRepoHandlers{}
	req := httptest.NewRequest("DELETE", "/api/saved-repos/some-id", nil)
	w := httptest.NewRecorder()
	h.DeleteSavedRepo(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIsDuplicateError(t *testing.T) {
	assert.False(t, isDuplicateError(nil))
	assert.True(t, isDuplicateError(&pq.Error{Code: "23505"}))
	assert.False(t, isDuplicateError(&pq.Error{Code: "23502"}))
	assert.False(t, isDuplicateError(fmt.Errorf("some other error")))
}
