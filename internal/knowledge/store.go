// Package knowledge provides storage for knowledge items.
package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// Store is the interface for knowledge item persistence.
type Store interface {
	Save(ctx context.Context, item model.KnowledgeItem) (model.KnowledgeItem, error)
	ListByTeam(ctx context.Context, teamID, status string) ([]model.KnowledgeItem, error)
	ListApprovedByWorkflow(ctx context.Context, teamID, workflowTemplateID string, maxItems int) ([]model.KnowledgeItem, error)
	UpdateStatus(ctx context.Context, id string, status model.KnowledgeStatus) error
	Delete(ctx context.Context, id string) error
}

// DBStore is the production PostgreSQL-backed Store.
type DBStore struct {
	db *sqlx.DB
}

// NewDBStore creates a new DBStore.
func NewDBStore(db *sqlx.DB) *DBStore {
	return &DBStore{db: db}
}

func (s *DBStore) Save(ctx context.Context, item model.KnowledgeItem) (model.KnowledgeItem, error) {
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	item.CreatedAt = time.Now()
	if item.Status == "" {
		item.Status = model.KnowledgeStatusPending
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_items (id, team_id, workflow_template_id, step_run_id, type, summary, details, source, tags, confidence, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, item.TeamID, nullStr(item.WorkflowTemplateID), nullStr(item.StepRunID),
		string(item.Type), item.Summary, item.Details, string(item.Source),
		item.Tags, item.Confidence, string(item.Status), item.CreatedAt,
	)
	if err != nil {
		return item, fmt.Errorf("save knowledge item: %w", err)
	}
	return item, nil
}

func (s *DBStore) ListByTeam(ctx context.Context, teamID, status string) ([]model.KnowledgeItem, error) {
	var items []model.KnowledgeItem
	var err error
	if status != "" {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items WHERE team_id=$1 AND status=$2 ORDER BY created_at DESC`,
			teamID, status)
	} else {
		err = s.db.SelectContext(ctx, &items,
			`SELECT * FROM knowledge_items WHERE team_id=$1 ORDER BY created_at DESC`, teamID)
	}
	return items, err
}

func (s *DBStore) ListApprovedByWorkflow(ctx context.Context, teamID, workflowTemplateID string, maxItems int) ([]model.KnowledgeItem, error) {
	if maxItems <= 0 {
		maxItems = 10
	}
	var items []model.KnowledgeItem
	err := s.db.SelectContext(ctx, &items,
		`SELECT * FROM knowledge_items
		 WHERE team_id=$1 AND status='approved'
		   AND (workflow_template_id=$2 OR workflow_template_id IS NULL)
		 ORDER BY confidence DESC LIMIT $3`,
		teamID, workflowTemplateID, maxItems)
	return items, err
}

func (s *DBStore) UpdateStatus(ctx context.Context, id string, status model.KnowledgeStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_items SET status=$1 WHERE id=$2`, string(status), id)
	return err
}

func (s *DBStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_items WHERE id=$1`, id)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// MemoryStore is an in-memory Store for unit tests.
type MemoryStore struct {
	mu    sync.Mutex
	items []model.KnowledgeItem
}

// NewMemoryStore creates a new in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Save(_ context.Context, item model.KnowledgeItem) (model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	s.items = append(s.items, item)
	return item, nil
}

func (s *MemoryStore) ListByTeam(_ context.Context, teamID, status string) ([]model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.KnowledgeItem
	for _, item := range s.items {
		if item.TeamID != teamID {
			continue
		}
		if status != "" && string(item.Status) != status {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemoryStore) ListApprovedByWorkflow(_ context.Context, teamID, workflowTemplateID string, maxItems int) ([]model.KnowledgeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.KnowledgeItem
	for _, item := range s.items {
		if item.TeamID != teamID {
			continue
		}
		if item.Status != model.KnowledgeStatusApproved {
			continue
		}
		if item.WorkflowTemplateID != "" && item.WorkflowTemplateID != workflowTemplateID {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if maxItems > 0 && len(out) > maxItems {
		out = out[:maxItems]
	}
	return out, nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id string, status model.KnowledgeStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			s.items[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("item %s not found", id)
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return nil
		}
	}
	return nil
}

// FormatEnrichmentBlock formats approved knowledge items as a prompt context block.
func FormatEnrichmentBlock(items []model.KnowledgeItem) string {
	if len(items) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Knowledge Base\n\nThe following insights from previous runs may be relevant:\n\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("**[%s]** %s\n", item.Type, item.Summary))
		if item.Details != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", item.Details))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
