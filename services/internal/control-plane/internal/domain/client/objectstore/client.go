// Package objectstore задаёт узкий порт immutable Instruction и Schedule prompt artifacts.
package objectstore

import "context"

// Object — подтверждённый version-pinned S3 object без credential values.
type Object struct {
	Reference string
	VersionID string
	SHA256    string
	Size      uint64
	MediaType string
}

// Client записывает и проверяет immutable content до owner transaction.
type Client interface {
	Check(context.Context) error
	Put(context.Context, string, string, []byte, string, string) (Object, error)
}
