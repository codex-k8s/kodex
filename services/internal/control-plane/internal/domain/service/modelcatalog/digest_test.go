package modelcatalog

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

// TestCatalogDigestCanonicalAndVersionBound checks account-order stability and commitment to
// availability, effort defaults/list, eligible accounts and source revision, including a nonempty
// commitment for an empty catalog.
// TestCatalogDigestCanonicalAndVersionBound проверяет устойчивость к порядку аккаунтов и привязку
// к доступности, efforts и их default, допустимым аккаунтам и source revision, включая непустой
// commitment пустого каталога.
func TestCatalogDigestCanonicalAndVersionBound(t *testing.T) {
	models := []entity.ModelCapability{{ID: "model", ProviderDefinitionKey: "provider", Available: true, ReasoningEfforts: []string{"low", "high"}, EligibleProviderAccountRefs: []string{"b", "a"}}}
	first, err := Digest(models, "version-1")
	if err != nil || len(first) != 64 {
		t.Fatal("invalid digest")
	}
	models[0].EligibleProviderAccountRefs = []string{"a", "b"}
	if next, _ := Digest(models, "version-1"); next != first {
		t.Fatal("account order changed digest")
	}
	for _, mutate := range []func(*entity.ModelCapability){
		func(m *entity.ModelCapability) { m.Available = false },
		func(m *entity.ModelCapability) { m.DefaultReasoningEffort = "high" },
		func(m *entity.ModelCapability) { m.ReasoningEfforts = []string{"low"} },
		func(m *entity.ModelCapability) { m.EligibleProviderAccountRefs = []string{"a"} },
	} {
		copy := models[0]
		mutate(&copy)
		if next, _ := Digest([]entity.ModelCapability{copy}, "version-1"); next == first {
			t.Fatal("semantic change lost")
		}
	}
	if next, _ := Digest(models, "version-2"); next == first {
		t.Fatal("source revision lost")
	}
	if a, _ := Digest(nil); a == "" {
		t.Fatal("empty catalog has no commitment")
	}
}

// TestCapabilityDigestIgnoresOtherModelsButBindsSelectedCapabilities checks selected-model effort
// sorting is stable and changes to account/model, supported efforts, default effort or
// default-model flag invalidate the capability pin.
// TestCapabilityDigestIgnoresOtherModelsButBindsSelectedCapabilities проверяет устойчивость
// сортировки efforts выбранной модели и изменение capability pin при смене аккаунта, модели,
// допустимых efforts, default effort либо флага default модели.
func TestCapabilityDigestIgnoresOtherModelsButBindsSelectedCapabilities(t *testing.T) {
	base := CapabilityDigest("provider", "pacc_one", "selected", []string{"medium", "low"}, "medium", true)
	if len(base) != 64 || CapabilityDigest("provider", "pacc_one", "selected", []string{"low", "medium"}, "medium", true) != base {
		t.Fatal("selected model digest is not canonical")
	}
	for _, changed := range []string{
		CapabilityDigest("provider", "pacc_other", "selected", []string{"low", "medium"}, "medium", true),
		CapabilityDigest("provider", "pacc_one", "other", []string{"low", "medium"}, "medium", true),
		CapabilityDigest("provider", "pacc_one", "selected", []string{"medium"}, "medium", true),
		CapabilityDigest("provider", "pacc_one", "selected", []string{"low", "medium"}, "low", true),
		CapabilityDigest("provider", "pacc_one", "selected", []string{"low", "medium"}, "medium", false),
	} {
		if changed == base {
			t.Fatal("selected model capability change did not invalidate pin")
		}
	}
}
