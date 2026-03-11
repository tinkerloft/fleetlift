package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueAndValidate(t *testing.T) {
	secret := []byte("test-secret")
	token, err := IssueToken(secret, "user-1", map[string]string{"team-1": "admin"}, false)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := ValidateToken(secret, token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "admin", claims.TeamRoles["team-1"])
}

func TestExpiredToken(t *testing.T) {
	claims := Claims{
		UserID: "user-1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString([]byte("test-secret"))

	_, err := ValidateToken([]byte("test-secret"), tokenStr)
	require.Error(t, err)
}
