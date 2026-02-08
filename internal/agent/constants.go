package agent

import "time"

// Polling intervals.
const (
	ManifestPollInterval = 500 * time.Millisecond
	SteeringPollInterval = 2 * time.Second
)

// Default values.
const (
	DefaultCloneDepth  = 50
	DefaultMaxSteering = 5
)

// Truncation limits.
const (
	MaxOutputTruncation      = 10000
	MaxSteeringContextChars  = 4000
)
