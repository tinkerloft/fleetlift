package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONMap_ScanNil(t *testing.T) {
	var m JSONMap
	require.NoError(t, m.Scan(nil))
	assert.Nil(t, m)
}

func TestJSONMap_ScanBytes(t *testing.T) {
	var m JSONMap
	require.NoError(t, m.Scan([]byte(`{"key":"value","num":42}`)))
	assert.Equal(t, "value", m["key"])
	assert.Equal(t, float64(42), m["num"])
}

func TestJSONMap_Value(t *testing.T) {
	m := JSONMap{"key": "value"}
	v, err := m.Value()
	require.NoError(t, err)
	assert.Contains(t, string(v.([]byte)), `"key"`)
}

func TestJSONMap_ValueNil(t *testing.T) {
	var m JSONMap
	v, err := m.Value()
	require.NoError(t, err)
	assert.Nil(t, v)
}
