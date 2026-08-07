package secretstore

import "context"

type Version struct {
	Ref           string
	Version       uint64
	ContentDigest string
}

type Store interface {
	Put(context.Context, string, []byte) (Version, error)
	Get(context.Context, string) ([]byte, Version, error)
	Revoke(context.Context, string, uint64) error
	Check(context.Context) error
}
