package nodepullidentity

import "testing"

func TestCommonNameMatchesVaultSubdomainRole(t *testing.T) {
	t.Parallel()

	commonName := CommonName("node-production-1", 7)
	if commonName != "6ba7078c0476fe89.g7.kodex-node-pull" {
		t.Fatalf("unexpected common name: %s", commonName)
	}
	if !ValidCommonName(commonName, 7) {
		t.Fatal("generated common name was rejected")
	}
}

func TestCommonNameValidationRejectsExpandedIdentity(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		generation uint64
	}{
		{"kodex-node-pull-3bd0c4f120d4bf48-g7", 7},
		{"3bd0c4f120d4bf48.g8.kodex-node-pull", 7},
		{"3bd0c4f120d4bf4.g7.kodex-node-pull", 7},
		{"3BD0C4F120D4BF48.g7.kodex-node-pull", 7},
		{"3bd0c4f120d4bf48.g7.other-node-pull", 7},
		{"3bd0c4f120d4bf48.g7.kodex-node-pull", 0},
	} {
		if ValidCommonName(testCase.name, testCase.generation) {
			t.Fatalf("unsafe common name was accepted: %s", testCase.name)
		}
	}
}
