package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitError_Error(t *testing.T) {
	err := &ExitError{Code: 1, Stderr: "something failed"}
	assert.Equal(t, "exit code 1: something failed", err.Error())
}

func TestExitError_TypeAssertion(t *testing.T) {
	var err error = &ExitError{Code: 2, Stderr: "bad"}
	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, 2, exitErr.Code)
}

// mockFS implements FileSystem for testing.
type mockFS struct {
	mu    sync.Mutex
	files map[string][]byte

	readFileFunc  func(string) ([]byte, error)
	writeFileFunc func(string, []byte, os.FileMode) error
	mkdirAllFunc  func(string, os.FileMode) error
	removeFunc    func(string) error
	renameFunc    func(string, string) error
	statFunc      func(string) (os.FileInfo, error)
}

func newMockFS() *mockFS {
	return &mockFS{files: make(map[string][]byte)}
}

func (m *mockFS) ReadFile(path string) ([]byte, error) {
	if m.readFileFunc != nil {
		return m.readFileFunc(path)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}

func (m *mockFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	if m.writeFileFunc != nil {
		return m.writeFileFunc(path, data, perm)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = data
	return nil
}

func (m *mockFS) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirAllFunc != nil {
		return m.mkdirAllFunc(path, perm)
	}
	return nil
}

func (m *mockFS) Remove(path string) error {
	if m.removeFunc != nil {
		return m.removeFunc(path)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, path)
	return nil
}

func (m *mockFS) Rename(oldpath, newpath string) error {
	if m.renameFunc != nil {
		return m.renameFunc(oldpath, newpath)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[oldpath]
	if !ok {
		return fmt.Errorf("file not found: %s", oldpath)
	}
	m.files[newpath] = data
	delete(m.files, oldpath)
	return nil
}

func (m *mockFS) Stat(path string) (os.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(path)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; ok {
		return nil, nil
	}
	return nil, fmt.Errorf("file not found: %s", path)
}

// mockExecutor implements CommandExecutor for testing.
type mockExecutor struct {
	mu      sync.Mutex
	calls   []CommandOpts
	runFunc func(ctx context.Context, opts CommandOpts) (*CommandResult, error)
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{}
}

func (m *mockExecutor) Run(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, opts)
	m.mu.Unlock()

	if m.runFunc != nil {
		return m.runFunc(ctx, opts)
	}
	return &CommandResult{Stdout: "", Stderr: "", ExitCode: 0}, nil
}

func (m *mockExecutor) getCalls() []CommandOpts {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]CommandOpts, len(m.calls))
	copy(result, m.calls)
	return result
}
