package main

import (
	"strings"
	"testing"
)

func TestRotationAbortArgumentsFailClosed(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := []string{"rotation-abort", "--intent-id", "13900000-0000-4000-8000-000000000001", "--source-revision", "2", "--source-digest-sha256", digest, "--confirm", "ABORT-STAGING-AUTHORITY-ROTATION"}
	action, err := parseCommand(valid)
	if err != nil || action != commandRotationAbort {
		t.Fatal("rotation abort command rejected")
	}
	if _, err := parseRotationOptions(action, valid); err != nil {
		t.Fatal("rotation abort identity rejected")
	}
	for _, tc := range []struct {
		index int
		value string
	}{
		{1, "--force"}, {2, "00000000-0000-0000-0000-000000000000"},
		{4, "02"}, {4, "0"}, {6, strings.Repeat("A", 64)},
		{8, "ABORT-PRODUCTION-AUTHORITY-ROTATION"},
	} {
		args := append([]string(nil), valid...)
		args[tc.index] = tc.value
		if _, err := parseRotationOptions(action, args); err == nil {
			t.Fatalf("invalid rotation abort accepted: argument %d", tc.index)
		}
	}
	if _, err := parseCommand(valid[:8]); err == nil {
		t.Fatal("incomplete rotation abort accepted")
	}
	if _, err := parseCommand(append(valid, "--force")); err == nil {
		t.Fatal("additional rotation abort argument accepted")
	}
}
