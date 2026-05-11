package secretresolver

import (
	"context"
	"fmt"
)

// Mux routes safe secret references to concrete backends by store type.
type Mux struct {
	backends map[string]Backend
}

// NewMux creates a resolver from a store-type backend map.
func NewMux(backends map[string]Backend) (*Mux, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("%w: no secret backends configured", ErrUnsupportedStoreType)
	}
	copied := make(map[string]Backend, len(backends))
	for storeType, backend := range backends {
		normalizedStoreType := normalizeStoreType(storeType)
		if normalizedStoreType == "" {
			return nil, ErrInvalidRef
		}
		if backend == nil {
			return nil, fmt.Errorf("%w: nil secret backend", ErrUnsupportedStoreType)
		}
		copied[normalizedStoreType] = backend
	}
	return &Mux{backends: copied}, nil
}

// Resolve returns a secret value for an already authorized reference.
func (m *Mux) Resolve(ctx context.Context, ref SecretRef) (SecretValue, error) {
	normalized, backend, err := m.backend(ref)
	if err != nil {
		return SecretValue{}, err
	}
	return backend.Resolve(ctx, normalized)
}

// Check verifies that a reference is available without exposing the value.
func (m *Mux) Check(ctx context.Context, ref SecretRef) (SecretStatus, error) {
	normalized, backend, err := m.backend(ref)
	if err != nil {
		return SecretStatus{}, err
	}
	return backend.Check(ctx, normalized)
}

func (m *Mux) backend(ref SecretRef) (SecretRef, Backend, error) {
	if m == nil {
		return SecretRef{}, nil, fmt.Errorf("%w: nil secret resolver", ErrUnsupportedStoreType)
	}
	normalized, err := normalizeRef(ref)
	if err != nil {
		return SecretRef{}, nil, err
	}
	backend, ok := m.backends[normalized.StoreType]
	if !ok {
		return SecretRef{}, nil, ErrUnsupportedStoreType
	}
	return normalized, backend, nil
}
