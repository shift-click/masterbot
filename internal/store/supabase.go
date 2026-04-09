package store

import (
	"context"
	"errors"
)

var ErrSupabaseNotImplemented = errors.New("supabase store is not implemented yet")

type SupabaseStore struct {
	url   string
	key   string
	table string
}

func NewSupabaseStore(url, key, table string) *SupabaseStore {
	return &SupabaseStore{
		url:   url,
		key:   key,
		table: table,
	}
}

func (s *SupabaseStore) Get(context.Context, string) ([]byte, error) {
	return nil, ErrSupabaseNotImplemented
}

func (s *SupabaseStore) Set(context.Context, string, []byte) error {
	return ErrSupabaseNotImplemented
}

func (s *SupabaseStore) Delete(context.Context, string) error {
	return ErrSupabaseNotImplemented
}

func (s *SupabaseStore) List(context.Context, string) (map[string][]byte, error) {
	return nil, ErrSupabaseNotImplemented
}
