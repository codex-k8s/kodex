package runtimecontract

import (
	"strings"
	"testing"
)

// TestRuntimeRevisionDigestBindsImmutableSnapshotAndExcludesLease changes prompt and credential-source versions to prove binding while confirming that lease renewal does not alter the revision.
// TestRuntimeRevisionDigestBindsImmutableSnapshotAndExcludesLease меняет версии промпта и источника реквизитов для проверки привязки и подтверждает, что обновление аренды не меняет ревизию.
func TestRuntimeRevisionDigestBindsImmutableSnapshotAndExcludesLease(t *testing.T) {
	input := validRunnerInputFixture()
	input.RuntimeRevisionRef = "rrev_abcdefgh"
	input.RuntimeRevisionVersion = 7
	source := RuntimeRevisionCredentialSource{SecretName: "provider-auth", SecretUID: "uid-1", SecretResourceVersion: "19"}
	baseline, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}

	changed := input
	changed.PromptTemplateDigest = strings.Repeat("e", 64)
	if digest, _ := RuntimeRevisionDigest(changed, source); digest == baseline {
		t.Fatal("prompt revision change did not change RuntimeRevision digest")
	}
	changed = input
	changed.LeaseRef = "lea_other000"
	changed.LeaseFence = "fnc_other000"
	changed.LeaseGeneration++
	if digest, _ := RuntimeRevisionDigest(changed, source); digest != baseline {
		t.Fatal("execution lease changed immutable RuntimeRevision digest")
	}
	source.SecretResourceVersion = "20"
	if digest, _ := RuntimeRevisionDigest(input, source); digest == baseline {
		t.Fatal("credential source revision did not change RuntimeRevision digest")
	}
}

// TestRuntimeBoundedInputDigestIsCanonical requires equal map content to produce the same digest regardless of key insertion order.
// TestRuntimeBoundedInputDigestIsCanonical требует одинакового дайджеста для равного содержимого словарей независимо от порядка добавления ключей.
func TestRuntimeBoundedInputDigestIsCanonical(t *testing.T) {
	left, err := RuntimeBoundedInputDigest(map[string]any{"b": float64(2), "a": "one"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := RuntimeBoundedInputDigest(map[string]any{"a": "one", "b": float64(2)})
	if err != nil || left != right {
		t.Fatalf("canonical input digest mismatch: left=%q right=%q err=%v", left, right, err)
	}
}
