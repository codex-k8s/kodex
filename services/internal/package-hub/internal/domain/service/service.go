// Package service contains package-hub use cases.
package service

// Service is the package-hub application service boundary.
//
// PKG-3.1 intentionally keeps use cases empty: stable gRPC contracts already
// exist, while repository-backed catalog and installation operations are added
// by the following package-hub slices.
type Service struct{}

// New creates an empty package-hub service boundary for the process skeleton.
func New() *Service {
	return &Service{}
}
