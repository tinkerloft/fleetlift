package activity

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"go.temporal.io/sdk/temporal"

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

// GetBatch retrieves and decrypts multiple credentials in a single query.
// Returns a map of credential name → plaintext value.
// Returns an error if any requested credential is not found.
func (s *DBCredentialStore) GetBatch(ctx context.Context, teamID string, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}

	type row struct {
		Name     string `db:"name"`
		ValueEnc []byte `db:"value_enc"`
	}
	var rows []row
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT name, value_enc FROM credentials WHERE team_id = $1 AND name = ANY($2)`,
		teamID, pq.Array(names),
	); err != nil {
		return nil, fmt.Errorf("batch query credentials: %w", err)
	}

	found := make(map[string]string, len(rows))
	for _, r := range rows {
		plaintext, err := flcrypto.DecryptAESGCM(s.encryptionKey, r.ValueEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt credential %q: %w", r.Name, err)
		}
		found[r.Name] = plaintext
	}

	// Verify all requested names were found.
	for _, name := range names {
		if _, ok := found[name]; !ok {
			return nil, fmt.Errorf("credential %q not found for team %s", name, teamID)
		}
	}
	return found, nil
}

// ValidateCredentials checks that all named credentials exist for the given team.
// Returns a non-retryable error for missing credentials so Temporal does not
// retry permanently-absent values. Transient DB errors remain retryable.
func (a *Activities) ValidateCredentials(ctx context.Context, teamID string, credNames []string) error {
	if len(credNames) == 0 || a.CredStore == nil {
		return nil
	}
	if _, err := a.CredStore.GetBatch(ctx, teamID, credNames); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("preflight credential check failed: %v", err),
				"CREDENTIAL_NOT_FOUND",
				err,
			)
		}
		return fmt.Errorf("preflight credential check failed: %w", err)
	}
	return nil
}
