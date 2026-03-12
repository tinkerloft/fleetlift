package activity

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/jmoiron/sqlx"

	flcrypto "github.com/tinkerloft/fleetlift/internal/crypto"
)

// DBCredentialStore implements CredentialStore by reading from the credentials
// table and decrypting values with AES-256-GCM.
type DBCredentialStore struct {
	db            *sqlx.DB
	encryptionKey string // hex-encoded 32-byte AES-256 key
}

// NewDBCredentialStore creates a new DBCredentialStore. The encryptionKey must be
// a 64-character hex-encoded string representing a 32-byte AES-256 key.
func NewDBCredentialStore(db *sqlx.DB, encryptionKey string) (*DBCredentialStore, error) {
	key, err := hex.DecodeString(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return &DBCredentialStore{db: db, encryptionKey: encryptionKey}, nil
}

// Get retrieves and decrypts a credential by team ID and name.
func (s *DBCredentialStore) Get(ctx context.Context, teamID, name string) (string, error) {
	var valueEnc []byte
	err := s.db.GetContext(ctx, &valueEnc,
		`SELECT value_enc FROM credentials WHERE team_id = $1 AND name = $2`,
		teamID, name)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("credential %q not found for team %s", name, teamID)
	}
	if err != nil {
		return "", fmt.Errorf("query credential: %w", err)
	}

	plaintext, err := flcrypto.DecryptAESGCM(s.encryptionKey, valueEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt credential %q: %w", name, err)
	}
	return plaintext, nil
}
