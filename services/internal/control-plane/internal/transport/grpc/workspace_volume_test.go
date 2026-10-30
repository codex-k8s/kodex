package grpc

import (
	"encoding/json"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

// workspaceVolumeTransportFixture supplies an owner-shaped CREATE result whose private lease
// remains outside the immutable physical work; its fixed past expiry tests historical replay.
// workspaceVolumeTransportFixture задаёт owner-shaped CREATE result с private lease вне immutable
// physical work; фиксированный прошлый срок проверяет исторический replay.
func workspaceVolumeTransportFixture() map[string]any {
	work := runtimecontract.WorkspaceVolumeWork{TaskRef: "svt_create0000", Kind: runtimecontract.WorkspaceVolumeCreate, Attempt: 1,
		Volume: runtimecontract.WorkspaceVolumeInput{Profile: runtimecontract.WorkspaceVolumeLocalPathProfile,
			Scope:        runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"},
			WorkspaceRef: "wks_fixture000", VolumeGeneration: 1, CreateTaskRef: "svt_create0000"}}
	work.InputDigest, _ = work.Digest()
	return map[string]any{"work": work, "leaseRef": "lea_fixture000", "fence": "fixture-private-fence", "generation": int64(1),
		"expiresAt": time.Date(2026, 1, 1, 1, 2, 3, 123456000, time.UTC)}
}

// TestWorkspaceVolumeClaimReceiptPreservesWireTuple proves JSON receipt replay retains the
// complete original work, lease and nanosecond expiry without refreshing an expired lease.
// TestWorkspaceVolumeClaimReceiptPreservesWireTuple доказывает сохранение полного original work,
// lease и nanosecond expiry после JSON receipt replay без обновления истёкшего lease.
func TestWorkspaceVolumeClaimReceiptPreservesWireTuple(t *testing.T) {
	values := workspaceVolumeTransportFixture()
	fresh := castWorkspaceVolumeTask(values)
	raw, err := json.Marshal(values)
	var replay map[string]any
	if err != nil || json.Unmarshal(raw, &replay) != nil {
		t.Fatal("fixture receipt encoding failed")
	}
	historical := castWorkspaceVolumeTask(replay)
	if fresh == nil || historical == nil || !proto.Equal(fresh, historical) ||
		fresh.GetLease().GetExpiresAt().AsTime() != values["expiresAt"].(time.Time) {
		t.Fatal("durable receipt changed the immutable task or exact lease expiry")
	}
	for name, mutate := range map[string]func(map[string]any){
		"fractional-generation": func(v map[string]any) { v["generation"] = 1.5 },
		"missing-expiry":        func(v map[string]any) { delete(v, "expiresAt") },
		"invalid-expiry":        func(v map[string]any) { v["expiresAt"] = "invalid" },
		"zero-expiry":           func(v map[string]any) { v["expiresAt"] = time.Time{} },
		"borrowed-generation":   func(v map[string]any) { v["generation"] = 2 },
		"missing-fence":         func(v map[string]any) { delete(v, "fence") },
		"unknown-work-field": func(v map[string]any) {
			v["work"].(map[string]any)["authority"] = "caller-selected"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			if json.Unmarshal(raw, &changed) != nil {
				t.Fatal("fixture receipt decoding failed")
			}
			mutate(changed)
			if castWorkspaceVolumeTask(changed) != nil {
				t.Fatal("malformed receipt acquired physical-work authority")
			}
		})
	}
}

// TestWorkspaceVolumeMutationRequiresStableReplayKey rejects absent keys, unknown wire fields
// and even explicit zero OCC instead of deriving a new claim from a correlation identifier.
// TestWorkspaceVolumeMutationRequiresStableReplayKey отклоняет missing keys, unknown wire fields
// и даже explicit zero OCC вместо создания нового claim по correlation identifier.
func TestWorkspaceVolumeMutationRequiresStableReplayKey(t *testing.T) {
	zero := int64(0)
	unknown := &cp.MutationContext{IdempotencyKey: "fixture-key"}
	unknown.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
	for _, mutation := range []*cp.MutationContext{nil, {}, {IdempotencyKey: " \n "},
		{IdempotencyKey: "fixture-key", ExpectedVersion: &zero}, unknown} {
		if workspaceVolumeMutation(mutation) {
			t.Fatal("ambiguous mutation admitted without a stable closed receipt")
		}
	}
	if !workspaceVolumeMutation(&cp.MutationContext{IdempotencyKey: "fixture-key"}) {
		t.Fatal("explicit receipt key rejected")
	}
}

// TestWorkspaceVolumeReadyRejectsUnknownNestedWire prevents a newer or forged nested field from
// being silently discarded before the owner validates the physical observation.
// TestWorkspaceVolumeReadyRejectsUnknownNestedWire не позволяет незаметно отбросить newer/forged
// nested field перед owner validation физического observation.
func TestWorkspaceVolumeReadyRejectsUnknownNestedWire(t *testing.T) {
	ready := &cp.RuntimeWorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Device: 1, Inode: 2,
		Preparation: &cp.RuntimeWorkspaceVolumePreparation{Scope: &cp.RuntimeWorkspaceScope{OrganizationRef: "org_fixture000",
			ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}, WorkspaceRef: "wks_fixture000",
			TaskRef: "svt_prepare000", PvcUid: "10000000-0000-4000-8000-000000000001", VolumeGeneration: 1}}
	if decoded, valid := decodeWorkspaceVolumeReady(ready); !valid || decoded == nil {
		t.Fatal("exact sealed observation rejected")
	}
	for _, depth := range []int{0, 1, 2} {
		changed := proto.Clone(ready).(*cp.RuntimeWorkspaceVolumeReady)
		message := proto.Message(changed)
		if depth == 1 {
			message = changed.Preparation
		} else if depth == 2 {
			message = changed.Preparation.Scope
		}
		message.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
		if _, valid := decodeWorkspaceVolumeReady(changed); valid {
			t.Fatal("unknown nested field was silently dropped")
		}
	}
}
