package value

import "testing"

func TestValidImageRepository(t *testing.T) {
	t.Parallel()
	if !ValidImageRepository("registry-pull.invalid/mattercodex/roles") {
		t.Fatal("canonical promoted repository was rejected")
	}
	for _, suffix := range []string{"@sha256:deadbeef", "?query", "#fragment", " name", "\\name", "\rname", "\nname", "\tname"} {
		if ValidImageRepository("registry-pull.invalid/mattercodex/roles" + suffix) {
			t.Fatalf("forbidden repository suffix was accepted: %q", suffix)
		}
	}
}
