package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusBadRequest, "invalid input")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.JSONEq(t, `{"error":"invalid input"}`, w.Body.String())
}

func TestWriteJSONError_InternalServer(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusInternalServerError, "something broke")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"error":"something broke"}`, w.Body.String())
}
