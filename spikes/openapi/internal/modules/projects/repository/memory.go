// Package repository holds storage adapters for projects. The spike uses an
// in-memory store; a real app uses sqlc and PostgreSQL behind the same port.
package repository

import (
	"context"
	"sort"
	"sync"

	projectdomain "apistock.dev/spikes/openapi/internal/modules/projects/domain"
	projectusecase "apistock.dev/spikes/openapi/internal/modules/projects/usecase"
)

var _ projectusecase.ProjectRepository = (*Memory)(nil)

// Memory is an in-memory ProjectRepository. It is safe for concurrent use.
type Memory struct {
	mu   sync.Mutex
	byID map[string]projectdomain.Project
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]projectdomain.Project)}
}

// Insert stores p, rejecting duplicate names within the organisation.
func (m *Memory) Insert(_ context.Context, p projectdomain.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.byID {
		if existing.OrgID == p.OrgID && existing.Name == p.Name {
			return projectdomain.ErrNameTaken
		}
	}
	m.byID[p.ID] = p
	return nil
}

// Get returns the project only if it belongs to orgID.
func (m *Memory) Get(_ context.Context, orgID, id string) (projectdomain.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byID[id]
	if !ok || p.OrgID != orgID {
		return projectdomain.Project{}, projectdomain.ErrNotFound
	}
	return p, nil
}

// List returns the organisation's projects ordered by creation time.
func (m *Memory) List(_ context.Context, orgID string) ([]projectdomain.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []projectdomain.Project{}
	for _, p := range m.byID {
		if p.OrgID == orgID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// Update replaces a project of the same organisation.
func (m *Memory) Update(_ context.Context, p projectdomain.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.byID[p.ID]
	if !ok || existing.OrgID != p.OrgID {
		return projectdomain.ErrNotFound
	}
	m.byID[p.ID] = p
	return nil
}
