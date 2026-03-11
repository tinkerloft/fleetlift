package activity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/jmoiron/sqlx"
)

// DBCredentialStore implements CredentialStore by reading from the credentials
// table and decrypting values with AES-256-GCM.
type DBCredentialStore struct {
	db  *sqlx.DB
	key []byte // 32-byte AES-256 key
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
	return &DBCredentialStore{db: db, key: key}, nil
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

	plaintext, err := decryptAESGCM(s.key, valueEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt credential %q: %w", name, err)
	}
	return string(plaintext), nil
}

// decryptAESGCM decrypts ciphertext using AES-256-GCM.
// The ciphertext format is: nonce (12 bytes) || encrypted data.
func decryptAESGCM(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// EncryptAESGCM encrypts plaintext using AES-256-GCM.
// Returns: nonce (12 bytes) || encrypted data.
// Exported for use by credential management APIs.
func EncryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}
