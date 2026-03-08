package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

// Protocol handles file-based communication between the worker and the sidecar agent.
type Protocol struct {
	basePath string
	fs       FileSystem
}

// NewProtocol creates a new Protocol with the given base path and filesystem.
func NewProtocol(basePath string, fs FileSystem) *Protocol {
	return &Protocol{basePath: basePath, fs: fs}
}

// WaitForManifest polls until the manifest file appears, then returns its raw JSON.
func (p *Protocol) WaitForManifest(ctx context.Context) (json.RawMessage, error) {
	path := fleetproto.ManifestPath(p.basePath)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		data, err := p.fs.ReadFile(path)
		if err == nil {
			return json.RawMessage(data), nil
		}
		time.Sleep(ManifestPollInterval)
	}
}

// WriteStatus atomically writes the agent status file.
func (p *Protocol) WriteStatus(status fleetproto.AgentStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	tmpPath := fleetproto.StatusPath(p.basePath) + ".tmp"
	if err := p.fs.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write status tmp: %w", err)
	}
	return p.fs.Rename(tmpPath, fleetproto.StatusPath(p.basePath))
}

// WriteResult atomically writes the agent result file.
func (p *Protocol) WriteResult(result json.RawMessage) error {
	tmpPath := fleetproto.ResultPath(p.basePath) + ".tmp"
	if err := p.fs.WriteFile(tmpPath, result, 0644); err != nil {
		return fmt.Errorf("write result tmp: %w", err)
	}
	return p.fs.Rename(tmpPath, fleetproto.ResultPath(p.basePath))
}

// WaitForSteering polls until a steering file appears, atomically consumes it, and returns the instruction.
func (p *Protocol) WaitForSteering(ctx context.Context) (*fleetproto.SteeringInstruction, error) {
	steeringPath := fleetproto.SteeringPath(p.basePath)
	processingPath := steeringPath + ".processing"
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if err := p.fs.Rename(steeringPath, processingPath); err != nil {
			time.Sleep(SteeringPollInterval)
			continue
		}
		data, err := p.fs.ReadFile(processingPath)
		_ = p.fs.Remove(processingPath)
		if err != nil {
			continue
		}
		var instruction fleetproto.SteeringInstruction
		if err := json.Unmarshal(data, &instruction); err != nil {
			continue
		}
		return &instruction, nil
	}
}
