package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListener_FanOut(t *testing.T) {
	l := &Listener{subs: make(map[string][]chan struct{})}
	ch1 := l.Subscribe("run-1")
	ch2 := l.Subscribe("run-1")
	l.fanOut("run-1")
	assert.Len(t, ch1, 1)
	assert.Len(t, ch2, 1)
}

func TestListener_UnsubscribeCleans(t *testing.T) {
	l := &Listener{subs: make(map[string][]chan struct{})}
	ch := l.Subscribe("run-1")
	l.Unsubscribe("run-1", ch)
	l.fanOut("run-1")
	assert.Len(t, ch, 0)
}

func TestListener_FanOutIsolatesRuns(t *testing.T) {
	l := &Listener{subs: make(map[string][]chan struct{})}
	ch1 := l.Subscribe("run-1")
	ch2 := l.Subscribe("run-2")
	l.fanOut("run-1")
	assert.Len(t, ch1, 1)
	assert.Len(t, ch2, 0, "run-2 should not be notified for run-1 event")
}
