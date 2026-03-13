package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
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

// openTestDB opens a DB connection for integration tests.
// Tests that call this are skipped unless DATABASE_URL is set.
func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("requires DB: set DATABASE_URL")
	}
	db, err := sqlx.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// insertTestUser inserts a minimal user row and returns its UUID, cleaning up on test end.
func insertTestUser(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	var id string
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (name, provider, provider_id) VALUES ('test', 'test', $1) RETURNING id`,
		t.Name(),
	).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func TestRotateRefreshToken_SingleUse(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db)

	raw, err := IssueRefreshToken(ctx, db, userID)
	require.NoError(t, err)

	// First rotation succeeds
	newToken, gotUserID, err := RotateRefreshToken(ctx, db, raw)
	require.NoError(t, err)
	assert.Equal(t, userID, gotUserID)
	assert.NotEmpty(t, newToken)

	// Second rotation on the original token fails
	_, _, err = RotateRefreshToken(ctx, db, raw)
	require.Error(t, err)
}

func TestRotateRefreshToken_Expired(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db)

	// Issue a token then back-date its expiry
	raw, err := IssueRefreshToken(ctx, db, userID)
	require.NoError(t, err)
	hash := sha256hex(raw)
	_, err = db.ExecContext(ctx,
		`UPDATE refresh_tokens SET expires_at = $1 WHERE token_hash = $2`,
		time.Now().Add(-time.Minute), hash)
	require.NoError(t, err)

	_, _, err = RotateRefreshToken(ctx, db, raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestRotateRefreshToken_ReuseInvalidatesAll(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db)

	// Issue two tokens for the same user
	raw1, err := IssueRefreshToken(ctx, db, userID)
	require.NoError(t, err)
	_, err = IssueRefreshToken(ctx, db, userID)
	require.NoError(t, err)

	// Rotate raw1 once legitimately
	_, _, err = RotateRefreshToken(ctx, db, raw1)
	require.NoError(t, err)

	// Rotate raw1 again — reuse attack; should delete all tokens
	_, _, err = RotateRefreshToken(ctx, db, raw1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reuse")

	// Confirm no tokens remain for this user
	var count int
	err = db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1`, userID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
