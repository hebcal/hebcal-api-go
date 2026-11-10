package yahrzeitdb

import (
	"context"
	"sync"
)

// Memory is an in-memory Store for tests. Err, when set, is returned by every
// Get, standing in for an unreachable database.
type Memory struct {
	mu      sync.Mutex
	Rows    map[string]*Row
	Err     error
	touched []string
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, id string) (*Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	row, ok := m.Rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *row
	return &cp, nil
}

// Touch implements Store, marking the row downloaded and recording the call.
func (m *Memory) Touch(id string, downloaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.Rows[id]; ok && !downloaded {
		row.Downloaded = true
	}
	m.touched = append(m.touched, id)
}

// Touched returns the ids Touch was called with, in order.
func (m *Memory) Touched() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.touched...)
}
