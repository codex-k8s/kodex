package authority

import "testing"

func TestContinuationGrantVersionAllowsOnlyExactOrImmediateReplay(t *testing.T) {
	for _, test := range []struct {
		name                         string
		currentVersion, currentFence uint64
		grantedVersion, grantedFence uint64
		allowed                      bool
	}{
		{name: "exact", currentVersion: 7, currentFence: 9, grantedVersion: 7, grantedFence: 9, allowed: true},
		{name: "immediate replay", currentVersion: 8, currentFence: 10, grantedVersion: 7, grantedFence: 9, allowed: true},
		{name: "two transitions", currentVersion: 9, currentFence: 11, grantedVersion: 7, grantedFence: 9},
		{name: "version without fence", currentVersion: 8, currentFence: 9, grantedVersion: 7, grantedFence: 9},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := continuationGrantVersionAllowed(test.currentVersion, test.currentFence,
				test.grantedVersion, test.grantedFence); got != test.allowed {
				t.Fatalf("continuationGrantVersionAllowed() = %v, want %v", got, test.allowed)
			}
		})
	}
}
