package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
	// Reject MCP-scoped tokens — they must use ValidateMCPToken instead.
	aud, _ := claims.GetAudience()
	for _, a := range aud {
		if a == "mcp" {
			return nil, fmt.Errorf("MCP tokens cannot be used for user authentication")
		}
	}
	return claims, nil
}

const refreshTokenTTL = 30 * 24 * time.Hour

var (
	ErrRefreshTokenInvalid       = errors.New("invalid refresh token")
	ErrRefreshTokenReuseDetected = errors.New("refresh token reuse detected")
	ErrRefreshTokenExpired       = errors.New("refresh token expired")
)

// IssueRefreshToken generates a secure random refresh token, stores its hash in the DB, and returns the raw token.
func IssueRefreshToken(ctx context.Context, db *sqlx.DB, userID string) (string, error) {
	return issueRefreshToken(ctx, db, userID)
}

// RotateRefreshToken validates a refresh token, marks it used, and issues a new one.
// If the token has already been used (reuse attack), all tokens for that user are invalidated.
func RotateRefreshToken(ctx context.Context, db *sqlx.DB, raw string) (newToken, userID string, err error) {
	newToken, _, userID, err = RefreshSession(ctx, db, raw, func(context.Context, sqlx.ExtContext, string) (string, error) {
		return "", nil
	})
	return newToken, userID, err
}

// RefreshSession validates a refresh token, lets the caller build a new access token,
// then consumes the old refresh token and inserts a replacement in one transaction.
func RefreshSession(
	ctx context.Context,
	db *sqlx.DB,
	raw string,
	buildAccessToken func(context.Context, sqlx.ExtContext, string) (string, error),
) (newRefreshToken, accessToken, userID string, err error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return "", "", "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rt, err := loadRefreshTokenForUpdate(ctx, tx, raw)
	if err != nil {
		return "", "", "", err
	}
	if rt.UsedAt != nil {
		if _, delErr := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, rt.UserID); delErr != nil {
			return "", "", "", fmt.Errorf("invalidate tokens on reuse: %w", delErr)
		}
		return "", "", "", ErrRefreshTokenReuseDetected
	}
	if time.Now().After(rt.ExpiresAt) {
		return "", "", "", ErrRefreshTokenExpired
	}

	accessToken, err = buildAccessToken(ctx, tx, rt.UserID)
	if err != nil {
		return "", "", "", err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET used_at = NOW() WHERE id = $1`, rt.ID); err != nil {
		return "", "", "", fmt.Errorf("mark refresh token used: %w", err)
	}

	newRefreshToken, err = issueRefreshToken(ctx, tx, rt.UserID)
	if err != nil {
		return "", "", "", err
	}

	if err := tx.Commit(); err != nil {
		return "", "", "", err
	}

	return newRefreshToken, accessToken, rt.UserID, nil
}

type refreshTokenRecord struct {
	ID        string     `db:"id"`
	UserID    string     `db:"user_id"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
}

func loadRefreshTokenForUpdate(ctx context.Context, q sqlx.QueryerContext, raw string) (*refreshTokenRecord, error) {
	hash := sha256hex(raw)
	var rt refreshTokenRecord
	if err := sqlx.GetContext(ctx, q, &rt,
		`SELECT id, user_id, expires_at, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, hash,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRefreshTokenInvalid
		}
		return nil, fmt.Errorf("load refresh token: %w", err)
	}
	return &rt, nil
}

func issueRefreshToken(ctx context.Context, exec sqlx.ExtContext, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(raw)
	hash := sha256hex(token)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, time.Now().Add(refreshTokenTTL))
	return token, err
}

// sha256hex returns the hex-encoded SHA-256 hash of s.
func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
