package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONMap is a map[string]any that scans from and serializes to PostgreSQL JSONB.
type JSONMap map[string]any

func (j *JSONMap) Scan(src any) error {
	if src == nil {
		*j = nil
		return nil
	}
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("JSONMap.Scan: unsupported type %T", src)
	}
	return json.Unmarshal(b, j)
}

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
