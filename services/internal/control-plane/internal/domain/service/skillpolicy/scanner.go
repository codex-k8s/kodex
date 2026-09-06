package skillpolicy

import "context"

type ScanVerdict struct {
	Engine   string
	Infected bool
}

type Scanner interface {
	Scan(context.Context, []byte) (ScanVerdict, error)
}
