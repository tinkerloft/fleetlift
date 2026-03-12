package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortContainerID(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string returns placeholder",
			input:    "",
			expected: "<empty>",
		},
		{
			name:     "short ID returned as-is",
			input:    "abc123",
			expected: "abc123",
		},
		{
			name:     "exactly 12 chars returned as-is",
			input:    "abcdef123456",
			expected: "abcdef123456",
		},
		{
			name:     "long ID truncated to 12 chars",
			input:    "abcdef1234567890",
			expected: "abcdef123456",
		},
		{
			name:     "typical docker container ID truncated",
			input:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
			expected: "a1b2c3d4e5f6",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, shortContainerID(c.input))
		})
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	t.Run("returns env var when set", func(t *testing.T) {
		t.Setenv("TEST_KEY_ABC", "myvalue")
		assert.Equal(t, "myvalue", getEnvOrDefault("TEST_KEY_ABC", "default"))
	})

	t.Run("returns default when env var not set", func(t *testing.T) {
		assert.Equal(t, "fallback", getEnvOrDefault("TEST_KEY_DEFINITELY_NOT_SET_XYZ", "fallback"))
	})

	t.Run("returns default when env var is empty string", func(t *testing.T) {
		t.Setenv("TEST_KEY_EMPTY", "")
		assert.Equal(t, "default", getEnvOrDefault("TEST_KEY_EMPTY", "default"))
	})
}

func TestParseMemory(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected int64
		wantErr  bool
	}{
		{
			name:     "empty string returns default 4GB",
			input:    "",
			expected: 4 * 1024 * 1024 * 1024,
		},
		{
			name:     "4g parses to 4GB",
			input:    "4g",
			expected: 4 * 1024 * 1024 * 1024,
		},
		{
			name:     "4G (uppercase) parses to 4GB",
			input:    "4G",
			expected: 4 * 1024 * 1024 * 1024,
		},
		{
			name:     "2g parses to 2GB",
			input:    "2g",
			expected: 2 * 1024 * 1024 * 1024,
		},
		{
			name:     "512m parses to 512MB",
			input:    "512m",
			expected: 512 * 1024 * 1024,
		},
		{
			name:     "1gi parses to 1GiB",
			input:    "1gi",
			expected: 1024 * 1024 * 1024,
		},
		{
			name:     "1024mi parses to 1024MiB",
			input:    "1024mi",
			expected: 1024 * 1024 * 1024,
		},
		{
			name:     "1024k parses to 1024KB",
			input:    "1024k",
			expected: 1024 * 1024,
		},
		{
			name:     "1ki parses to 1KiB",
			input:    "1ki",
			expected: 1024,
		},
		{
			name:     "invalid value returns error and default",
			input:    "notanumber",
			expected: 4 * 1024 * 1024 * 1024,
			wantErr:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseMemory(c.input)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, c.expected, got)
		})
	}
}

func TestParseCPU(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected int64
		wantErr  bool
	}{
		{
			name:     "empty string returns default 2 CPUs",
			input:    "",
			expected: 200000,
		},
		{
			name:     "2 CPUs",
			input:    "2",
			expected: 200000,
		},
		{
			name:     "1 CPU",
			input:    "1",
			expected: 100000,
		},
		{
			name:     "0.5 CPUs",
			input:    "0.5",
			expected: 50000,
		},
		{
			name:     "500m millicores = 0.5 CPU",
			input:    "500m",
			expected: 50000,
		},
		{
			name:     "1000m millicores = 1 CPU",
			input:    "1000m",
			expected: 100000,
		},
		{
			name:     "2000m millicores = 2 CPUs",
			input:    "2000m",
			expected: 200000,
		},
		{
			name:     "invalid value returns error and default",
			input:    "notanumber",
			expected: 200000,
			wantErr:  true,
		},
		{
			name:     "invalid millicore value returns error and default",
			input:    "xm",
			expected: 200000,
			wantErr:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseCPU(c.input)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, c.expected, got)
		})
	}
}
