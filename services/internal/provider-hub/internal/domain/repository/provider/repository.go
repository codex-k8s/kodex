package provider

import "context"

// Repository is the storage boundary owned by provider-hub.
//
// Business methods are added together with concrete provider workflows. The
// initial scaffold keeps only the readiness contract needed by the process.
type Repository interface {
	Ping(context.Context) error
}
