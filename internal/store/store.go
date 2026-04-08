package store

import "context"

type Store interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte) error
	Delete(context.Context, string) error
	List(context.Context, string) (map[string][]byte, error)
}
