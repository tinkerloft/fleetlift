package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ExitError represents a command that exited with a non-zero exit code.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit code %d: %s", e.Code, e.Stderr)
}

// FileSystem abstracts filesystem operations for testability.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Rename(oldpath, newpath string) error
	Stat(path string) (os.FileInfo, error)
}

// CommandExecutor abstracts command execution for testability.
type CommandExecutor interface {
	Run(ctx context.Context, opts CommandOpts) (*CommandResult, error)
}

// CommandOpts configures a command execution.
type CommandOpts struct {
	Name  string
	Args  []string
	Dir   string
	// Env, if non-empty, REPLACES the process environment entirely.
	// Use os.Environ() as a base and append to augment rather than replace.
	Env   []string
	Stdin io.Reader
}

// CommandResult holds the output of a command execution.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// osFileSystem implements FileSystem using the real OS.
type osFileSystem struct{}

func (osFileSystem) ReadFile(path string) ([]byte, error)                  { return os.ReadFile(path) }
func (osFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error { return os.WriteFile(path, data, perm) }
func (osFileSystem) MkdirAll(path string, perm os.FileMode) error         { return os.MkdirAll(path, perm) }
func (osFileSystem) Remove(path string) error                             { return os.Remove(path) }
func (osFileSystem) Rename(oldpath, newpath string) error                 { return os.Rename(oldpath, newpath) }
func (osFileSystem) Stat(path string) (os.FileInfo, error)                { return os.Stat(path) }

// osCommandExecutor implements CommandExecutor using os/exec.
type osCommandExecutor struct{}

func (osCommandExecutor) Run(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
	cmd := exec.CommandContext(ctx, opts.Name, opts.Args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return &CommandResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: -1,
			}, err
		}
	}

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}

	if exitCode != 0 {
		return result, &ExitError{Code: exitCode, Stderr: stderr.String()}
	}

	return result, nil
}
