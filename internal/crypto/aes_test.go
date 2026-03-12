package crypto_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	crypto "github.com/tinkerloft/fleetlift/internal/crypto"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
	ct, err := crypto.EncryptAESGCM(key, "secret-value")
	require.NoError(t, err)
	pt, err := crypto.DecryptAESGCM(key, ct)
	require.NoError(t, err)
	assert.Equal(t, "secret-value", pt)
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	key := strings.Repeat("ab", 32)
	ct1, _ := crypto.EncryptAESGCM(key, "same")
	ct2, _ := crypto.EncryptAESGCM(key, "same")
	assert.NotEqual(t, ct1, ct2)
}
