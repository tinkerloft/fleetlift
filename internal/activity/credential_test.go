package activity

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	flcrypto "github.com/tinkerloft/fleetlift/internal/crypto"
)

func TestAESGCMRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	keyHex := hex.EncodeToString(key)
	plaintext := "my-secret-api-key-12345"

	ciphertext, err := flcrypto.EncryptAESGCM(keyHex, plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, []byte(plaintext), ciphertext)
	assert.Greater(t, len(ciphertext), len(plaintext))

	decrypted, err := flcrypto.DecryptAESGCM(keyHex, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestAESGCMWrongKey(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	keyHex := hex.EncodeToString(key)

	ciphertext, err := flcrypto.EncryptAESGCM(keyHex, "secret")
	require.NoError(t, err)

	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(i + 1)
	}
	wrongKeyHex := hex.EncodeToString(wrongKey)

	_, err = flcrypto.DecryptAESGCM(wrongKeyHex, ciphertext)
	assert.Error(t, err)
}

func TestNewDBCredentialStore_InvalidKey(t *testing.T) {
	_, err := NewDBCredentialStore(nil, "tooshort")
	assert.Error(t, err)

	_, err = NewDBCredentialStore(nil, "not-hex-at-all-definitely-not-valid-hex-string!!")
	assert.Error(t, err)
}

func TestNewDBCredentialStore_ValidKey(t *testing.T) {
	key := make([]byte, 32)
	hexKey := hex.EncodeToString(key)

	store, err := NewDBCredentialStore(nil, hexKey)
	require.NoError(t, err)
	assert.NotNil(t, store)
	assert.Equal(t, hexKey, store.encryptionKey)
}

func TestValidateCredentials_NoCredentials_ReturnsNil(t *testing.T) {
	a := &Activities{CredStore: &stubCredStore{}}
	err := a.ValidateCredentials(context.Background(), "team-1", nil)
	require.NoError(t, err)
}

func TestValidateCredentials_AllPresent_ReturnsNil(t *testing.T) {
	a := &Activities{CredStore: &stubCredStore{val: "secret"}}
	err := a.ValidateCredentials(context.Background(), "team-1", []string{"GITHUB_TOKEN", "API_KEY"})
	require.NoError(t, err)
}

func TestValidateCredentials_MissingCredential_ReturnsError(t *testing.T) {
	// errCredStore (defined in actions_test.go) always returns an error from GetBatch.
	a := &Activities{CredStore: &errCredStore{}}
	err := a.ValidateCredentials(context.Background(), "team-1", []string{"GITHUB_TOKEN"})
	require.Error(t, err)
}
