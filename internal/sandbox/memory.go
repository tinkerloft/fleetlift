package sandbox

import (
	"context"
	"fmt"
)

// MemoryClient is an in-memory sandbox client for tests.
type MemoryClient struct {
	files map[string][]byte
}

// NewMemoryClient creates a MemoryClient with preset file contents.
// Keys are file paths; values are file contents.
func NewMemoryClient(files map[string][]byte) *MemoryClient {
	if files == nil {
		files = make(map[string][]byte)
	}
	return &MemoryClient{files: files}
}

func (m *MemoryClient) Create(_ context.Context, _ CreateOpts) (string, error) {
	return "memory-sandbox-id", nil
}

func (m *MemoryClient) ExecStream(_ context.Context, _, _, _ string, onLine func(string)) error {
	return nil
}

func (m *MemoryClient) Exec(_ context.Context, _, _, _ string) (stdout, stderr string, err error) {
	return "", "", nil
}

func (m *MemoryClient) WriteFile(_ context.Context, _, path, content string) error {
	m.files[path] = []byte(content)
	return nil
}

func (m *MemoryClient) ReadFile(_ context.Context, _, path string) (string, error) {
	data, ok := m.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	return string(data), nil
}

func (m *MemoryClient) ReadBytes(_ context.Context, _, path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}

func (m *MemoryClient) Kill(_ context.Context, _ string) error {
	return nil
}

func (m *MemoryClient) RenewExpiration(_ context.Context, _ string) error {
	return nil
}
