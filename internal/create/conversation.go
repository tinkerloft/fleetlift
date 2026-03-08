package create

import (
	"context"
	"fmt"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // message text
}

// Conversation holds the state of a multi-turn AI chat session.
type Conversation struct {
	ID        string    `json:"id"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConversationStore manages active conversations with TTL-based cleanup.
type ConversationStore struct {
	mu    sync.RWMutex
	convs map[string]*Conversation
	ttl   time.Duration
}

// NewConversationStore creates a store with the given TTL.
func NewConversationStore(ttl time.Duration) *ConversationStore {
	s := &ConversationStore{
		convs: make(map[string]*Conversation),
		ttl:   ttl,
	}
	go s.cleanup()
	return s
}

// Get returns a conversation by ID, or nil if not found / expired.
func (s *ConversationStore) Get(id string) *Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.convs[id]
	if !ok || time.Since(c.UpdatedAt) > s.ttl {
		return nil
	}
	return c
}

// Put stores or updates a conversation.
func (s *ConversationStore) Put(c *Conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.UpdatedAt = time.Now()
	s.convs[c.ID] = c
}

// Delete removes a conversation.
func (s *ConversationStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.convs, id)
}

func (s *ConversationStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for id, c := range s.convs {
			if time.Since(c.UpdatedAt) > s.ttl {
				delete(s.convs, id)
			}
		}
		s.mu.Unlock()
	}
}

// toAnthropicMessages converts the conversation history to Anthropic API format.
func toAnthropicMessages(msgs []Message) []anthropic.MessageParam {
	params := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		role := anthropic.MessageParamRoleUser
		if m.Role == "assistant" {
			role = anthropic.MessageParamRoleAssistant
		}
		params = append(params, anthropic.MessageParam{
			Role: role,
			Content: []anthropic.ContentBlockParamUnion{
				{OfText: &anthropic.TextBlockParam{Text: m.Content}},
			},
		})
	}
	return params
}

// StreamCallback is called for each chunk of streaming text.
type StreamCallback func(text string) error

// StreamConversationMessage sends a user message and streams the response token by token.
// The full assistant reply is appended to the conversation and returned.
func StreamConversationMessage(
	ctx context.Context,
	client *anthropic.Client,
	conv *Conversation,
	userText string,
	callback StreamCallback,
) (string, error) {
	conv.Messages = append(conv.Messages, Message{Role: "user", Content: userText})

	systemPrompt := BuildInteractiveSystemPrompt()
	history := toAnthropicMessages(conv.Messages)

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  history,
	})

	var fullReply string
	for stream.Next() {
		event := stream.Current()
		delta := event.AsContentBlockDelta()
		if delta.Delta.Type == "text_delta" {
			fullReply += delta.Delta.Text
			if err := callback(delta.Delta.Text); err != nil {
				_ = stream.Close()
				return fullReply, err
			}
		}
	}
	if err := stream.Err(); err != nil {
		return fullReply, fmt.Errorf("streaming error: %w", err)
	}
	_ = stream.Close()

	conv.Messages = append(conv.Messages, Message{Role: "assistant", Content: fullReply})
	return fullReply, nil
}
