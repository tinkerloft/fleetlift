package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
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

const refreshTokenTTL = 30 * 24 * time.Hour

// IssueRefreshToken generates a secure random refresh token, stores its hash in the DB, and returns the raw token.
func IssueRefreshToken(ctx context.Context, db *sqlx.DB, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(raw)
	hash := sha256hex(token)
	_, err := db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, time.Now().Add(refreshTokenTTL))
	return token, err
}

// RotateRefreshToken validates a refresh token, marks it used, and issues a new one.
// If the token has already been used (reuse attack), all tokens for that user are invalidated.
func RotateRefreshToken(ctx context.Context, db *sqlx.DB, raw string) (newToken, userID string, err error) {
	hash := sha256hex(raw)
	var rt struct {
		ID        string     `db:"id"`
		UserID    string     `db:"user_id"`
		ExpiresAt time.Time  `db:"expires_at"`
		UsedAt    *time.Time `db:"used_at"`
	}
	if err = db.GetContext(ctx, &rt,
		`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1`, hash,
	); err != nil {
		return "", "", fmt.Errorf("invalid refresh token")
	}
	if rt.UsedAt != nil {
		// Token reuse detected — invalidate all tokens for this user
		if _, delErr := db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, rt.UserID); delErr != nil {
			return "", "", fmt.Errorf("invalidate tokens on reuse: %w", delErr)
		}
		return "", "", fmt.Errorf("refresh token reuse detected")
	}
	if time.Now().After(rt.ExpiresAt) {
		return "", "", fmt.Errorf("refresh token expired")
	}
	if _, updErr := db.ExecContext(ctx, `UPDATE refresh_tokens SET used_at = NOW() WHERE id = $1`, rt.ID); updErr != nil {
		return "", "", fmt.Errorf("mark refresh token used: %w", updErr)
	}
	newToken, err = IssueRefreshToken(ctx, db, rt.UserID)
	return newToken, rt.UserID, err
}

// sha256hex returns the hex-encoded SHA-256 hash of s.
func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
