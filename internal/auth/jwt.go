package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims holds the JWT payload for Fleetlift sessions.
type Claims struct {
	UserID        string            `json:"user_id"`
	TeamRoles     map[string]string `json:"team_roles"`
	PlatformAdmin bool              `json:"platform_admin"`
	jwt.RegisteredClaims
}

// IssueToken creates a signed JWT with the given claims.
func IssueToken(secret []byte, userID string, teamRoles map[string]string, platformAdmin bool) (string, error) {
	claims := Claims{
		UserID:        userID,
		TeamRoles:     teamRoles,
		PlatformAdmin: platformAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ValidateToken parses and validates a JWT string, returning the claims.
func ValidateToken(secret []byte, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
