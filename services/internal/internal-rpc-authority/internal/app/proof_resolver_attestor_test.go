package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProofSignerGenerationUsesCurrentSecretValue(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "current_generation")
	if err := os.WriteFile(path, []byte("2\n"), 0o440); err != nil {
		t.Fatalf("write generation fixture: %v", err)
	}
	generation, err := readProofSignerGeneration(path)
	if err != nil {
		t.Fatalf("readProofSignerGeneration() error = %v", err)
	}
	if generation != 2 {
		t.Fatalf("readProofSignerGeneration() = %d, want 2", generation)
	}
}

func TestReadProofSignerGenerationRejectsUnsafeOrInvalidValue(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct {
		value string
		mode  os.FileMode
	}{
		"zero":       {value: "0", mode: 0o440},
		"not-number": {value: "current", mode: 0o440},
		"writable":   {value: "2", mode: 0o600},
	} {
		fixture := fixture
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "current_generation")
			if err := os.WriteFile(path, []byte(fixture.value), fixture.mode); err != nil {
				t.Fatalf("write generation fixture: %v", err)
			}
			if _, err := readProofSignerGeneration(path); err == nil {
				t.Fatal("invalid proof signer generation accepted")
			}
		})
	}
}
