package activity

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAESGCMRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("my-secret-api-key-12345")

	ciphertext, err := EncryptAESGCM(key, plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)
	assert.Greater(t, len(ciphertext), len(plaintext))

	decrypted, err := decryptAESGCM(key, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestAESGCMWrongKey(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	ciphertext, err := EncryptAESGCM(key, []byte("secret"))
	require.NoError(t, err)

	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(i + 1)
	}

	_, err = decryptAESGCM(wrongKey, ciphertext)
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
	assert.Equal(t, key, store.key)
}
