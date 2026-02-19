// Package knowledge provides local persistent storage for knowledge items.
package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// Store manages knowledge items on the local filesystem.
// Layout: {BaseDir}/{task-id}/item-{item-id}.yaml
type Store struct {
	baseDir string
}

// NewStore creates a Store using the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// DefaultStore creates a Store using ~/.fleetlift/knowledge.
func DefaultStore() *Store {
	home, _ := os.UserHomeDir()
	return NewStore(filepath.Join(home, ".fleetlift", "knowledge"))
}

// BaseDir returns the base directory for this store.
func (s *Store) BaseDir() string {
	return s.baseDir
}

// Write persists a knowledge item for the given task ID.
func (s *Store) Write(taskID string, item model.KnowledgeItem) error {
	dir := filepath.Join(s.baseDir, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating knowledge dir: %w", err)
	}
	path := filepath.Join(dir, "item-"+item.ID+".yaml")
	data, err := yaml.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshaling knowledge item: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// List returns all knowledge items for a given task ID.
func (s *Store) List(taskID string) ([]model.KnowledgeItem, error) {
	dir := filepath.Join(s.baseDir, taskID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading knowledge dir: %w", err)
	}

	var items []model.KnowledgeItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		item, err := readItem(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip malformed items
		}
		items = append(items, item)
	}
	return items, nil
}

// ListAll returns all knowledge items across all tasks.
func (s *Store) ListAll() ([]model.KnowledgeItem, error) {
	entries, err := os.ReadDir(s.baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading knowledge base dir: %w", err)
	}

	var all []model.KnowledgeItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		items, err := s.List(e.Name())
		if err != nil {
			continue
		}
		all = append(all, items...)
	}
	return all, nil
}

// Delete removes a knowledge item by its ID (searches all task subdirs).
func (s *Store) Delete(itemID string) error {
	taskDirs, err := os.ReadDir(s.baseDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading knowledge base dir: %w", err)
	}

	target := "item-" + itemID + ".yaml"
	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		path := filepath.Join(s.baseDir, td.Name(), target)
		if err := os.Remove(path); err == nil {
			return nil
		}
	}
	return fmt.Errorf("knowledge item %q not found", itemID)
}

// FilterByTags returns up to maxItems knowledge items whose tags overlap with filterTags.
// If filterTags is empty, returns all items up to maxItems, sorted by confidence descending.
func (s *Store) FilterByTags(filterTags []string, maxItems int) ([]model.KnowledgeItem, error) {
	all, err := s.ListAll()
	if err != nil {
		return nil, err
	}

	tagSet := make(map[string]bool, len(filterTags))
	for _, t := range filterTags {
		tagSet[strings.ToLower(t)] = true
	}

	var matched []model.KnowledgeItem
	for _, item := range all {
		if len(tagSet) == 0 {
			matched = append(matched, item)
			continue
		}
		for _, t := range item.Tags {
			if tagSet[strings.ToLower(t)] {
				matched = append(matched, item)
				break
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Confidence > matched[j].Confidence
	})

	if maxItems > 0 && len(matched) > maxItems {
		matched = matched[:maxItems]
	}
	return matched, nil
}

// LoadFromRepo loads knowledge items from a transformation repository's
// .fleetlift/knowledge/items/ directory.
func LoadFromRepo(repoPath string) ([]model.KnowledgeItem, error) {
	dir := filepath.Join(repoPath, ".fleetlift", "knowledge", "items")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading repo knowledge dir: %w", err)
	}

	var items []model.KnowledgeItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		item, err := readItem(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func readItem(path string) (model.KnowledgeItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.KnowledgeItem{}, err
	}
	var item model.KnowledgeItem
	if err := yaml.Unmarshal(data, &item); err != nil {
		return model.KnowledgeItem{}, err
	}
	return item, nil
}
