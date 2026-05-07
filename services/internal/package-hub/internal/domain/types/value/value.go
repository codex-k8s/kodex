// Package value contains package-hub value objects.
package value

import "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"

type LocalizedText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

type SourceRef struct {
	Kind      enum.PackageVersionSourceRefKind
	Ref       string
	CommitSHA string
}

type PageRequest struct {
	PageSize  int32
	PageToken string
}

type PageResult struct {
	NextPageToken string
}
