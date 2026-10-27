package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// narrowSyntheticReviewOutput reduces only the synthetic review id maximum in place.
// Package copying, validation, versioning and production authority remain with the caller.
// narrowSyntheticReviewOutput уменьшает только maximum синтетического review id на месте.
// Копирование, проверка пакета, версия и production authority остаются у вызывающего.
func narrowSyntheticReviewOutput(pkg *integrationpackage.Package) {
	for index := range pkg.Spec.Capabilities {
		if pkg.Spec.Capabilities[index].Key == "github.pull_request.review.create" {
			for field := range pkg.Spec.Capabilities[index].OutputFields {
				if pkg.Spec.Capabilities[index].OutputFields[field].Key == "id" {
					pkg.Spec.Capabilities[index].OutputFields[field].Maximum--
				}
			}
		}
	}
}

// TestNarrowSyntheticReviewOutput checks the exact fixture mutation and unrelated fields.
// TestNarrowSyntheticReviewOutput проверяет точную мутацию fixture и незатронутые поля.
func TestNarrowSyntheticReviewOutput(t *testing.T) {
	pkg := integrationpackage.Package{Spec: integrationpackage.Spec{Capabilities: []integrationpackage.Capability{
		{Key: "github.pull_request.review.create", OutputFields: []integrationpackage.Field{{Key: "id", Maximum: 10}, {Key: "other", Maximum: 10}}},
		{Key: "other", OutputFields: []integrationpackage.Field{{Key: "id", Maximum: 10}}},
	}}}
	narrowSyntheticReviewOutput(&pkg)
	if pkg.Spec.Capabilities[0].OutputFields[0].Maximum != 9 || pkg.Spec.Capabilities[0].OutputFields[1].Maximum != 10 || pkg.Spec.Capabilities[1].OutputFields[0].Maximum != 10 {
		t.Fatal("synthetic review output mutation changed unrelated fields")
	}
}
