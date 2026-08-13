package policy

import "testing"

func TestProducerActorKindUsesCanonicalAuthorityKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		credential string
		expected   string
	}{
		{credential: "OIDC_BEARER", expected: "HUMAN"},
		{credential: "MATTERMOST_SIGNED_EVENT", expected: "HUMAN"},
		{credential: "AGENT_SESSION_GRANT", expected: "AGENT"},
		{credential: "AUTOMATION_OCCURRENCE_GRANT", expected: "AUTOMATION"},
		{credential: "WORKLOAD_READINESS_GRANT", expected: "SERVICE"},
		{credential: "INTEGRATION_CONTINUATION_GRANT", expected: "SERVICE"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.credential, func(t *testing.T) {
			t.Parallel()
			actual := producerActorKind(Producer{Credential: test.credential})
			if actual != test.expected {
				t.Fatalf("actor kind = %q, want %q", actual, test.expected)
			}
		})
	}
}
