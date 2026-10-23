package authorityproof

import (
	"strings"
	"testing"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// runtimeProofFixture builds synthetic owner authority and immutable execution metadata for actor-scope
// tests, without credentials, lease storage or a real runtime revision.
// runtimeProofFixture создаёт синтетические полномочия владельца и метаданные неизменяемого выполнения
// для тестов области исполнителя без учётных данных, хранилища lease и настоящей runtime-ревизии.
func runtimeProofFixture() platformrepo.ProofAuthority {
	return platformrepo.ProofAuthority{
		ActorID:        "10000000-0000-4000-8000-000000000001",
		OrganizationID: "10000000-0000-4000-8000-000000000002",
		ProjectID:      "10000000-0000-4000-8000-000000000003", ProjectVersion: 1,
		RuntimeExecution: &platformrepo.RuntimeExecutionProof{
			ActorKind: "HUMAN", RevisionID: "10000000-0000-4000-8000-000000000004",
			Generation: 7, RevisionDigest: strings.Repeat("a", 64),
		},
	}
}

// TestRuntimeExecutionActorUsesOwnerProvenance requires project and system-assistant materialization to
// preserve the owner's actor identity and exact runtime revision/generation/digest rather than a generic
// workload actor.
// TestRuntimeExecutionActorUsesOwnerProvenance требует сохранения личности владельца и точных ревизии,
// поколения и хеша выполнения при материализации проекта и помощника вместо общего исполнителя нагрузки.
func TestRuntimeExecutionActorUsesOwnerProvenance(t *testing.T) {
	for _, assistant := range []bool{false, true} {
		resolved := runtimeProofFixture()
		operation := "platform.runtime.credentials.materialize"
		if assistant {
			resolved.ProjectID = ""
			resolved.ProjectVersion = 0
			operation = "platform.runtime.credentials.system-assistant.materialize"
		}
		actor, kind, err := runtimeExecutionActor(resolved, "runtime-controller", operation)
		if err != nil || kind != "HUMAN" || actor.ID != resolved.ActorID ||
			actor.Provenance.Source != "RUNTIME_EXECUTION" || actor.Provenance.Reference != resolved.RuntimeExecution.RevisionID ||
			actor.Provenance.Revision != 7 || actor.Provenance.DigestSHA256 != resolved.RuntimeExecution.RevisionDigest {
			t.Fatalf("owner runtime provenance was not preserved: %v", err)
		}
	}
}

// TestRuntimeExecutionActorRejectsInvalidAuthority mutates missing/foreign/malformed owner lineage and
// scope inputs, requiring rejection before any proof is signed.
// TestRuntimeExecutionActorRejectsInvalidAuthority меняет отсутствующее, чужое или повреждённое
// происхождение и область владельца, требуя отказа до подписи доказательства.
func TestRuntimeExecutionActorRejectsInvalidAuthority(t *testing.T) {
	mutations := map[string]func(*platformrepo.ProofAuthority){
		"missing-execution":       func(value *platformrepo.ProofAuthority) { value.RuntimeExecution = nil },
		"invalid-root":            func(value *platformrepo.ProofAuthority) { value.ActorID = "caller-supplied" },
		"missing-project":         func(value *platformrepo.ProofAuthority) { value.ProjectID = "" },
		"missing-project-version": func(value *platformrepo.ProofAuthority) { value.ProjectVersion = 0 },
		"invalid-revision":        func(value *platformrepo.ProofAuthority) { value.RuntimeExecution.RevisionID = "rrev_locator" },
		"missing-generation":      func(value *platformrepo.ProofAuthority) { value.RuntimeExecution.Generation = 0 },
		"invalid-digest":          func(value *platformrepo.ProofAuthority) { value.RuntimeExecution.RevisionDigest = "invalid" },
		"unknown-actor-kind":      func(value *platformrepo.ProofAuthority) { value.RuntimeExecution.ActorKind = "OWNER" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			resolved := runtimeProofFixture()
			mutate(&resolved)
			if _, _, err := runtimeExecutionActor(resolved, "runtime-controller", "platform.runtime.credentials.materialize"); err == nil {
				t.Fatal("invalid runtime authority accepted")
			}
		})
	}
	for _, binding := range [][2]string{
		{"control-api-gateway", "platform.runtime.credentials.materialize"},
		{"runtime-controller", "platform.runtime.credentials.readiness.check"},
		{"runtime-controller", "platform.runtime.credentials.system-assistant.materialize"},
	} {
		if _, _, err := runtimeExecutionActor(runtimeProofFixture(), binding[0], binding[1]); err == nil {
			t.Fatal("runtime provenance accepted outside exact operation scope")
		}
	}
}

// TestRuntimeExecutionProofExpiryIsBounded covers producer/lease/grant minimums, expiry and subsecond
// truncation so an invalid or exhausted upstream window cannot become fresh proof lifetime.
// TestRuntimeExecutionProofExpiryIsBounded проверяет минимумы производителя, lease и grant, истечение и
// усечение долей секунды, чтобы неверное или исчерпанное окно не стало новым сроком доказательства.
func TestRuntimeExecutionProofExpiryIsBounded(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		lease, grant time.Duration
		want         time.Duration
		denied       bool
	}{
		{"producer", 30 * time.Second, time.Minute, 15 * time.Second, false},
		{"lease", 4*time.Second + 900*time.Millisecond, time.Minute, 4 * time.Second, false},
		{"grant", time.Minute, 3 * time.Second, 3 * time.Second, false},
		{"expired-lease", -time.Second, time.Minute, 0, true},
		{"expired-grant", time.Minute, -time.Second, 0, true},
		{"subsecond-lease", 900 * time.Millisecond, time.Minute, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			expiresAt, err := runtimeExecutionProofExpiry(now, now.Add(15*time.Second), now.Add(test.lease), now.Add(test.grant))
			if test.denied {
				if err == nil {
					t.Fatal("expired execution accepted")
				}
				return
			}
			if err != nil || !expiresAt.Equal(now.Add(test.want)) {
				t.Fatalf("incorrect proof deadline: %v", err)
			}
		})
	}
}
