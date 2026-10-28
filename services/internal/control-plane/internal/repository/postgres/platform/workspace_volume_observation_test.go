package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// workspaceVolumeAttemptFixture builds an owner frame independent of model revisions and serializes the ledger's empty digest slot.
// workspaceVolumeAttemptFixture строит owner frame отдельно от model revisions и сериализует пустой digest slot ledger.
func workspaceVolumeAttemptFixture(t *testing.T, kind string, attempt int32) (runtimecontract.WorkspaceVolumeWork, []byte, string) {
	t.Helper()
	work := runtimecontract.WorkspaceVolumeWork{TaskRef: "svt_create0000", Kind: kind, Attempt: attempt, ReconcileOnly: attempt > 1,
		Volume: runtimecontract.WorkspaceVolumeInput{Profile: runtimecontract.WorkspaceVolumeLocalPathProfile,
			Scope: runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000",
				SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}, WorkspaceRef: "wks_fixture000",
			VolumeGeneration: 1, CreateTaskRef: "svt_create0000"}}
	if kind == runtimecontract.WorkspaceVolumePrepare {
		work.TaskRef, work.Volume.PreparationTaskRef, work.Volume.PVCUID = "svt_prepare000", "svt_prepare000", "10000000-0000-4000-8000-000000000001"
	}
	digest, err := work.Digest()
	if err != nil {
		t.Fatal("owner volume frame fixture is invalid")
	}
	raw, err := json.Marshal(work)
	if err != nil {
		t.Fatal("encode owner volume frame fixture")
	}
	return work, raw, digest
}

// TestWorkspaceVolumeLedgerRestoresExactAttempt preserves the separately stored digest and every closed-union input across bounded attempts.
// TestWorkspaceVolumeLedgerRestoresExactAttempt сохраняет отдельно записанный digest и весь closed-union input при bounded attempts.
func TestWorkspaceVolumeLedgerRestoresExactAttempt(t *testing.T) {
	for _, kind := range []string{runtimecontract.WorkspaceVolumeCreate, runtimecontract.WorkspaceVolumePrepare} {
		for attempt := int32(1); attempt <= 5; attempt++ {
			work, raw, digest := workspaceVolumeAttemptFixture(t, kind, attempt)
			decoded, err := decodeWorkspaceVolumeAttemptSnapshot(raw, digest)
			work.InputDigest = digest
			if err != nil || decoded != work {
				t.Fatal("stored ledger lost its immutable effective attempt")
			}
		}
	}
}

// TestWorkspaceVolumeLedgerRejectsNoncanonicalFrame rejects ambiguous bytes even when an attacker recomputes their stored SHA256.
// TestWorkspaceVolumeLedgerRejectsNoncanonicalFrame отклоняет неоднозначные bytes даже при пересчитанном stored SHA256.
func TestWorkspaceVolumeLedgerRejectsNoncanonicalFrame(t *testing.T) {
	_, raw, digest := workspaceVolumeAttemptFixture(t, runtimecontract.WorkspaceVolumePrepare, 1)
	for name, bad := range map[string][]byte{
		"empty":           nil,
		"whitespace":      append(append([]byte(nil), raw...), '\n'),
		"trailing":        append(append([]byte(nil), raw...), []byte("{}")...),
		"alias":           bytes.Replace(raw, []byte(`"attempt"`), []byte(`"Attempt"`), 1),
		"duplicate":       bytes.Replace(raw, []byte(`"attempt":1`), []byte(`"attempt":1,"attempt":1`), 1),
		"unknown":         append([]byte(`{"unknown":true,`), raw[1:]...),
		"embedded-digest": bytes.Replace(raw, []byte(`"input_digest":""`), []byte(`"input_digest":"`+digest+`"`), 1),
		"invalid-attempt": bytes.Replace(raw, []byte(`"attempt":1`), []byte(`"attempt":0`), 1),
		"borrowed-write":  bytes.Replace(raw, []byte(`"task_ref":"svt_prepare000"`), []byte(`"task_ref":"svt_foreign000"`), 1),
		"oversize":        bytes.Repeat([]byte(" "), runtimecontract.MaximumWorkspaceVolumeReadyBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			sum := sha256.Sum256(bad)
			decoded, err := decodeWorkspaceVolumeAttemptSnapshot(bad, hex.EncodeToString(sum[:]))
			if !errors.Is(err, errs.ErrConflict) || decoded != (runtimecontract.WorkspaceVolumeWork{}) {
				t.Fatal("noncanonical ledger frame became an effective grant")
			}
		})
	}
	for _, badDigest := range []string{"", strings.Repeat("0", 64), strings.ToUpper(digest)} {
		if _, err := decodeWorkspaceVolumeAttemptSnapshot(raw, badDigest); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("ledger frame accepted an unrelated external digest")
		}
	}
}

// TestWorkspaceVolumeLedgerRejectsInvalidPinsBeforePersistence closes malformed references and generations before an attempted database read.
// TestWorkspaceVolumeLedgerRejectsInvalidPinsBeforePersistence закрывает malformed references/generations до обращения к database.
func TestWorkspaceVolumeLedgerRejectsInvalidPinsBeforePersistence(t *testing.T) {
	task := workspaceVolumeTask{ref: "svt_prepare000"}
	for _, input := range []command.WorkspaceVolumeTaskInput{
		{TaskRef: "svt_foreign000", LeaseRef: "lea_fixture000", Fence: "fnc_fixture000", Generation: 1},
		{TaskRef: task.ref, LeaseRef: "lea_fixture000", Fence: "fnc_fixture000", Generation: 0},
		{TaskRef: task.ref, LeaseRef: "lea_fixture000", Fence: "fnc_fixture000", Generation: 6},
		{TaskRef: task.ref, LeaseRef: "", Fence: "fnc_fixture000", Generation: 1},
		{TaskRef: task.ref, LeaseRef: "lea_fixture000", Fence: "", Generation: 1},
	} {
		if _, err := loadWorkspaceVolumeAttemptTx(t.Context(), nil, scope{}, task, input); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("invalid ledger pins reached persistence")
		}
	}
}
