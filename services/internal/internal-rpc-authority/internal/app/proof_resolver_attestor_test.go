package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadProofSignerGenerationUsesCurrentSecretValue writes a read-only temporary generation file containing two plus newline and checks that exact value is read. It tests file decoding, not a live Secret update, multi-version rotation or resolver attestation.
// TestReadProofSignerGenerationUsesCurrentSecretValue записывает во временный read-only файл поколение два с переводом строки и проверяет чтение этого значения. Проверяется разбор файла, а не живое обновление Secret, многоверсионная ротация или аттестация resolver.
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

// TestReadProofSignerGenerationRejectsUnsafeOrInvalidValue creates isolated temporary fixtures for zero, nonnumeric text and a writable file mode, requiring rejection in all three cases without reading real mounted material.
// TestReadProofSignerGenerationRejectsUnsafeOrInvalidValue создаёт изолированные временные эталоны нуля, нечислового текста и записываемого режима файла, требуя отказ во всех трёх случаях без чтения реальных смонтированных материалов.
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
