// Package objectstore задаёт узкий S3-compatible порт gateway.
package objectstore

import "context"

type Object struct {
	Reference string
	VersionID string
	SHA256    string
	Size      uint64
	Name      string
	MediaType string
}

type Client interface {
	Check(context.Context) error
	Put(context.Context, string, string, []byte, string, string) (Object, error)
	Inspect(context.Context, string, string, string) (Object, bool, error)
	Get(context.Context, string, string, uint64, string) ([]byte, error)
}
