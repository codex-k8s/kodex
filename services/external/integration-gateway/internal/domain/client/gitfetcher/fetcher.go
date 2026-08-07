package gitfetcher

import "context"

type (
	Fetched struct {
		Commit, SourceRef string
		Content           []byte
	}
	Client interface {
		Fetch(context.Context, string, string, string) (Fetched, error)
		Check(context.Context) error
	}
)
