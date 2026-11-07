package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"testing"
)

// TestPersonalCredentialProbeClaimPins copies owner-selected keys into the generated envelope,
// retaining absence for ordinary or legacy health claims and no alias into mutable owner data.
// TestPersonalCredentialProbeClaimPins копирует owner-selected keys в generated envelope,
// сохраняя отсутствие для обычных/legacy health claims и независимость от mutable owner data.
func TestPersonalCredentialProbeClaimPins(t *testing.T) {
	keys := []string{"fixture.health", "fixture.read"}
	claim := CastIntegrationConnectionTestClaim(map[string]any{"personalReadProbeCapabilityKeys": keys})
	keys[0] = "fixture.changed"
	if len(claim.PersonalReadProbeCapabilityKeys) != 2 || claim.PersonalReadProbeCapabilityKeys[0] != "fixture.health" {
		t.Fatal("probe keys lost their owner pins")
	}
	if len(CastIntegrationConnectionTestClaim(map[string]any{}).PersonalReadProbeCapabilityKeys) != 0 {
		t.Fatal("legacy health claim acquired extra probes")
	}
}

// TestPersonalCredentialMetadataClaimPins keeps metadata profiles typed and owner-derived,
// copies deniable keys and preserves unknown values for fail-closed consumer validation.
// TestPersonalCredentialMetadataClaimPins сохраняет metadata profiles typed и owner-derived,
// копирует deniable keys и оставляет unknown values для fail-closed consumer validation.
func TestPersonalCredentialMetadataClaimPins(t *testing.T) {
	profiles := []string{"GITLAB_PROJECT_WRITE_SCOPE_V1"}
	keys := []string{"gitlab.issue.create"}
	claim := CastIntegrationConnectionTestClaim(map[string]any{"personalObservationProfiles": profiles, "personalDeniedWriteCapabilityKeys": keys})
	profiles[0], keys[0] = "UNKNOWN", "gitlab.project.delete"
	if len(claim.PersonalObservationProfiles) != 1 || claim.PersonalObservationProfiles[0] != cp.PersonalIntegrationCredentialObservationProfile_PERSONAL_INTEGRATION_CREDENTIAL_OBSERVATION_PROFILE_GITLAB_PROJECT_WRITE_SCOPE_V1 || len(claim.PersonalDeniedWriteCapabilityKeys) != 1 || claim.PersonalDeniedWriteCapabilityKeys[0] != "gitlab.issue.create" {
		t.Fatal("typed metadata pins changed or share mutable state")
	}
	unknown := CastIntegrationConnectionTestClaim(map[string]any{"personalObservationProfiles": profiles})
	if len(unknown.PersonalObservationProfiles) != 1 || unknown.PersonalObservationProfiles[0] != cp.PersonalIntegrationCredentialObservationProfile_PERSONAL_INTEGRATION_CREDENTIAL_OBSERVATION_PROFILE_UNSPECIFIED {
		t.Fatal("unknown profile silently became supported")
	}
	legacy := CastIntegrationConnectionTestClaim(map[string]any{})
	if len(legacy.PersonalObservationProfiles) != 0 || len(legacy.PersonalDeniedWriteCapabilityKeys) != 0 {
		t.Fatal("legacy health claim acquired metadata requests")
	}
}
