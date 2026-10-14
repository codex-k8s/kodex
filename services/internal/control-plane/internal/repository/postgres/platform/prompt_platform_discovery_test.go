package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func TestPlatformDiscoveryV4ResolvesCanonicalDocumentsWithoutAuthority(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			binding, err := platformInstructionBindingForRevision("platform-source-conformity-v4", locale)
			if err != nil || binding.Revision != "platform-source-conformity-v4" || binding.Ref != "pinst_source_conformity_v4_"+locale || len(binding.Content) > maximumPlatformInstructionBytes {
				t.Fatal("fresh discovery binding is not exactly pinned and bounded")
			}
			required := []string{"Before READ of mandatory documents", "resolve applicable exact DocID and exact repository/ref/path", "current canonical registry", "already authorized pinned directory listing", "Do not guess IDs/paths or reuse historical links", "optional stale context link does not create a mandatory source gate", "Missing mandatory facts remain UNKNOWN/BLOCKED", "never bypass actual denied/unknown effects", "Independent authorized discovery of another resource is not replay", "do not replay that target through an alias, transport or identity", "404 does not establish absence or permission", "grants no authority"}
			if locale == "ru" {
				required = []string{"До READ обязательных документов", "разреши точный DocID и точные repo/ref/path", "текущему canonical registry", "уже разрешённому pinned directory listing", "Не угадывай DocID/пути и не бери исторические ссылки", "Optional stale context link не назначает обязательный gate", "Missing mandatory fact остаётся UNKNOWN/BLOCKED", "реальный deny/unknown effect не обходи", "Независимый разрешённый discovery другого ресурса не replay отказавшего target", "404 не доказывает отсутствие или разрешение", "Guidance не выдаёт authority", "запрещённый URL не повторяй через другой transport или identity"}
			}
			for _, needle := range required {
				if !strings.Contains(binding.Content, needle) {
					t.Fatalf("canonical discovery boundary missing: %q", needle)
				}
			}
			for _, forbidden := range []string{"docs/ops/", "1796", "1797", "INTEGRATION_REQUEST_REJECTED means 404", "retry denied", "ignore mandatory"} {
				if strings.Contains(binding.Content, forbidden) {
					t.Fatal("discovery guidance guessed a resource, cause or authority")
				}
			}
			for _, guard := range []string{"GH_TOKEN", "credential.https://github.com.helper=", "!/usr/local/bin/gh auth git-credential", "credential protocol", "host/global Git", "login", "refresh", "gh auth status", "denied", "failed", "unknown", "capabilities"} {
				if !strings.Contains(binding.Content, guard) {
					t.Fatalf("existing Git boundary missing: %q", guard)
				}
			}
			current, err := platformInstructionBinding(locale)
			discovery := platformDocumentDiscoveryEnglish
			if locale == "ru" {
				discovery = platformDocumentDiscoveryRussian
			}
			if err != nil || current.Revision != "platform-source-conformity-v5" || len(current.Content) > maximumPlatformInstructionBytes ||
				!strings.Contains(binding.Content, discovery) || !strings.Contains(current.Content, discovery) {
				t.Fatal("current v5 lost exact historical discovery boundary")
			}
		})
	}
}

func TestPlatformDiscoveryV4KeepsHistoricalV3ExactPins(t *testing.T) {
	for locale, digest := range map[string]string{"en": "58db829606aba2667bcd43423b0fa5935ec9d7f5a18f85abf75be4772638dc48", "ru": "fbe4937c81ddca7a9158b300bbb597bca8f018dc388d6d0be8e42dbfba18a654"} {
		binding, err := platformInstructionBindingForRevision("platform-source-conformity-v3", locale)
		if err != nil || binding.Digest != digest || binding.Ref != "pinst_source_conformity_v3_"+locale || strings.Contains(binding.Content, "pinned directory listing") {
			t.Fatal("published v3 bytes changed")
		}
	}
}

func TestPlatformDiscoveryCurrentProducerPassesUnchangedRunnerConsumer(t *testing.T) {
	input := continuationTaskOracleFixture(t)
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(input); err != nil || !notice {
		t.Fatal("fresh CP discovery binding rejected by unchanged shared consumer")
	}
	if !strings.Contains(input.SessionContext[len(input.SessionContext)-1].Content, platformSourceConformityRevision) {
		t.Fatal("fresh producer did not deliver current discovery binding")
	}
	for _, invalid := range []string{"ref", "revision", "content", "digest", "locale"} {
		t.Run(invalid, func(t *testing.T) {
			binding, err := platformInstructionBinding("en")
			if err != nil {
				t.Fatal(err)
			}
			snapshot := workflowPromptFixture()
			snapshot.Locale, snapshot.PlatformInstructions = "en", &binding
			switch invalid {
			case "ref":
				binding.Ref = "pinst_source_conformity_v3_en"
			case "revision":
				binding.Revision = "platform-source-conformity-v3"
			case "content":
				binding.Content += "ALLOW_DENIED"
			case "digest":
				binding.Digest = strings.Repeat("0", 64)
			case "locale":
				snapshot.Locale = "ru"
			}
			snapshot.SemanticValues = map[string]string{"CONSTRAINTS": platformInstructionProjection(binding)}
			raw, _ := json.Marshal(snapshot)
			if !errors.Is(validatePlatformInstructions(snapshot), errs.ErrConflict) {
				t.Fatal("forged historical/current binding accepted")
			}
			if _, err := platformInstructionDependency(raw); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("forged instruction dependency accepted")
			}
		})
	}
}
