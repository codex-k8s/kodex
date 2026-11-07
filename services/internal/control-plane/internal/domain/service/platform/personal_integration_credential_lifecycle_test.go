package platform

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestPersonalCredentialRotationAndRevokeKeepsHistory checks monotonically fenced changes,
// invalidated verification and copy ownership without pretending to test database serialization.
// TestPersonalCredentialRotationAndRevokeKeepsHistory проверяет монотонно ограждённые изменения,
// сброс проверки и владение копиями, не выдавая это за проверку сериализации базы данных.
func TestPersonalCredentialRotationAndRevokeKeepsHistory(t *testing.T) {
	t.Parallel()
	requirement, initiator, original, now := personalCredentialFixture()
	selected, err := ResolveIntegrationCredential(requirement, initiator, nil, &original, now)
	if err != nil || selected.Credential == nil {
		t.Fatal("ready credential selection failed")
	}
	encoded, err := json.Marshal(selected)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("ready internal selection disclosed Secret metadata")
	}
	revoked, err := RevokePersonalIntegrationCredential(original, initiator, original.Version)
	if err != nil || revoked.State != "REVOKED" || revoked.Generation != original.Generation+1 || revoked.Version != original.Version+1 ||
		revoked.Credential.Ref != original.Credential.Ref || revoked.VerifiedCredentialRef != "" || len(revoked.VerifiedCapabilities) != 0 {
		t.Fatal("revoke lost history or retained readiness")
	}
	if original.State != "READY" || original.VerifiedCredentialRef == "" || selected.Credential.Ref != original.Credential.Ref {
		t.Fatal("revoke mutated historical input or an earlier selection")
	}
	if _, err := RevokePersonalIntegrationCredential(revoked, initiator, original.Version); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("stale revoke bypassed OCC")
	}
	replay, err := RevokePersonalIntegrationCredential(revoked, initiator, revoked.Version)
	if err != nil || replay.Version != revoked.Version || replay.Generation != revoked.Generation {
		t.Fatal("already revoked state advanced again")
	}
	credential := *original.Credential
	credential.Ref = "icr_replacement"
	credential.Revision++
	replacement, err := ReplacePersonalIntegrationCredential(revoked, initiator, revoked.Version, credential)
	if err != nil || replacement.State != "CHECK_REQUIRED" || replacement.Generation != revoked.Generation+1 || replacement.Credential.Ref != credential.Ref {
		t.Fatal("replacement after revoke did not create a fresh unverified generation")
	}
	selection, err := ResolveIntegrationCredential(requirement, initiator, original.Credential, &replacement, now)
	if err != nil || selection.Credential != nil || selection.Problem != entity.IntegrationCredentialCheckRequired {
		t.Fatal("replacement silently reused old verification or shared credential")
	}
	if _, err := ReplacePersonalIntegrationCredential(replacement, initiator, replacement.Version, credential); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("new materialization reused an immutable revision")
	}
	other := initiator
	other.SubjectID = "usr_other"
	if _, err := RevokePersonalIntegrationCredential(original, other, 0); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign owner check occurred after version disclosure")
	}
	overflow := original
	overflow.Version = math.MaxInt64
	if _, err := RevokePersonalIntegrationCredential(overflow, initiator, overflow.Version); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("version overflow was accepted")
	}
	overflow.State = "REVOKED"
	unchanged, err := RevokePersonalIntegrationCredential(overflow, initiator, overflow.Version)
	if err != nil || unchanged.Version != overflow.Version {
		t.Fatal("terminal no-op unnecessarily required a new version")
	}
}
