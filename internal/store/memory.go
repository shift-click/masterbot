package store

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var ErrNotFound = errors.New("key not found")

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string][]byte),
	}
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, ok := s.data[key]
	if !ok {
		return nil, ErrNotFound
	}

	return append([]byte(nil), value...), nil
}

func (s *MemoryStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = append([]byte(nil), value...)
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	return nil
}

func (s *MemoryStore) List(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]byte)
	for key, value := range s.data {
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		result[key] = append([]byte(nil), value...)
	}

	return result, nil
}
