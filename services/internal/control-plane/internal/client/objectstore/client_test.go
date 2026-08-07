package objectstore

import "testing"

func TestInstructionObjectInputGuards(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"", "/absolute", "parent/../escape", "line\nbreak", "trailing/"} {
		if !invalidObjectKey(invalid) {
			t.Fatalf("unsafe object key was accepted: %q", invalid)
		}
	}
	if invalidObjectKey("instruction-sets/agent/content.md") {
		t.Fatal("safe object key was rejected")
	}
	if digest([]byte("content")) != "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73" {
		t.Fatal("content digest changed")
	}
}
