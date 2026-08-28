// Package objectstorage задаёт общий порт неизменяемого S3-compatible контента.
package objectstorage

import (
	"context"
	"errors"
	"io"
)

var (
	ErrInvalid     = errors.New("object storage input is invalid")
	ErrNotFound    = errors.New("object storage object is not found")
	ErrConflict    = errors.New("object storage object metadata conflicts with the request")
	ErrUnavailable = errors.New("object storage is unavailable")
)

type PutInput struct {
	Key, MediaType, Digest string
	SizeBytes              int64
	Body                   io.Reader
}

type Receipt struct {
	Key, VersionID, ETag, Digest string
	SizeBytes                    int64
}

type Object struct {
	Receipt
	Body io.ReadCloser
}

// Store никогда не раскрывает endpoint или credentials вызывающему домену.
// Key назначается сервером и содержит tenant boundary в своей структуре.
type Store interface {
	Check(context.Context) error
	Put(context.Context, PutInput) (Receipt, error)
	Get(context.Context, string, string) (Object, error)
	Head(context.Context, string, string) (Receipt, error)
	Delete(context.Context, string, string) error
}
