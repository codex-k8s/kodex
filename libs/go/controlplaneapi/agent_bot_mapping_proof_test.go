package controlplaneapi

import "testing"

func TestAgentBotMappingProofBindsExactCurrentTupleWithoutProviderID(t *testing.T) {
	t.Parallel()
	first, err := AgentBotMappingProofRef("11111111-1111-4111-8111-111111111111", 2, 3, 4, 5)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := AgentBotMappingProofRef("11111111-1111-4111-8111-111111111111", 2, 3, 4, 5)
	if err != nil || replay != first {
		t.Fatalf("mapping proof is not deterministic: %q %q %v", first, replay, err)
	}
	changed, err := AgentBotMappingProofRef("11111111-1111-4111-8111-111111111111", 3, 3, 4, 5)
	if err != nil || changed == first {
		t.Fatalf("stale mapping version reused proof: %q %q %v", first, changed, err)
	}
	if first == "mattermost-team-private" {
		t.Fatal("mapping proof exposed provider Team ID")
	}
}
