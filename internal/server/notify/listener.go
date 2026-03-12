package notify

import (
	"context"
	"sync"
	"time"

	"github.com/lib/pq"
)

// Listener subscribes to PostgreSQL LISTEN/NOTIFY on the "run_events" channel
// and fans out signals to per-run in-process subscribers.
type Listener struct {
	pql  *pq.Listener
	mu   sync.RWMutex
	subs map[string][]chan struct{}
}

// New creates a Listener using the given PostgreSQL connection string.
func New(connStr string) *Listener {
	l := &Listener{subs: make(map[string][]chan struct{})}
	l.pql = pq.NewListener(connStr, 10*time.Second, time.Minute, nil)
	return l
}

// Start begins listening for notifications. Blocks until ctx is cancelled.
func (l *Listener) Start(ctx context.Context) error {
	if err := l.pql.Listen("run_events"); err != nil {
		return err
	}
	for {
		select {
		case n, ok := <-l.pql.Notify:
			if !ok || n == nil {
				continue
			}
			l.fanOut(n.Extra)
		case <-ctx.Done():
			return l.pql.Close()
		}
	}
}

// Subscribe returns a channel that receives a signal when the given run is updated.
func (l *Listener) Subscribe(runID string) chan struct{} {
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	l.subs[runID] = append(l.subs[runID], ch)
	l.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (l *Listener) Unsubscribe(runID string, ch chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	subs := l.subs[runID]
	for i, s := range subs {
		if s == ch {
			l.subs[runID] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

// fanOut sends a non-blocking signal to all subscribers for runID.
func (l *Listener) fanOut(runID string) {
	l.mu.RLock()
	subs := l.subs[runID]
	l.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
