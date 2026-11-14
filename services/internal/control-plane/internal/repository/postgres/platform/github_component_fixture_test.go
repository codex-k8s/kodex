package platform

import (
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"testing"
)

// pinHistoricalGitHubComponent preserves the preexisting shared owner/repository fixture contract under its exact frozen6.4 version/digest. It never changes production defaults or introduces a second runtime implementation.
// pinHistoricalGitHubComponent сохраняет прежний shared owner/repository fixture contract по точным frozen6.4 version/digest. Не меняет production defaults и не вводит вторую runtime implementation.
func pinHistoricalGitHubComponent(t *testing.T, r *Repository) {
	t.Helper()
	current := r.integrationDefinitions["github"]
	previous, ok := integrationpackage.ResolveShippedRevision(current, "6.4.0", "bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da")
	if !ok {
		t.Fatal("frozen GitHub6.4 fixture contract is unavailable")
	}
	r.integrationDefinitions["github"] = previous
}

// currentGitHubComponent restores only the trusted currently shipped6.5 contract before Bootstrap for the new personal scope component scenarios.
// currentGitHubComponent возвращает лишь trusted currently shipped6.5 contract перед Bootstrap для новых personal scope component scenarios.
func currentGitHubComponent(t *testing.T) func(*Repository) {
	t.Helper()
	return func(r *Repository) {
		definitions, err := integrationpackage.LoadShipped()
		if err != nil {
			t.Fatal(err)
		}
		if definitions["github"].Metadata.Version != "6.5.0" {
			t.Fatal("current GitHub scope fixture has an unexpected version")
		}
		r.integrationDefinitions["github"] = definitions["github"]
	}
}
