package app

import "testing"

// TestSSHHealthCompositionRemainsClosedWithoutOperator requires explicit complete configuration and never enables a nil purpose service or guesses a target from product input.
// TestSSHHealthCompositionRemainsClosedWithoutOperator требует explicit complete configuration без nil purpose service либо target из product input.
func TestSSHHealthCompositionRemainsClosedWithoutOperator(t *testing.T) {
	service, err := newSSHIdentityHealth(Config{}, nil, nil)
	if err != nil || service != nil {
		t.Fatal("disabled health did not stay closed")
	}
	for _, config := range []Config{{SSHQualificationFile: "/foreign/profiles.json"}, {SSHQualificationSHA256: "partial"}} {
		if service, err := newSSHIdentityHealth(config, nil, nil); err == nil || service != nil {
			t.Fatal("partial health configuration admitted")
		}
	}
}
