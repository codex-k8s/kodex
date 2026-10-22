package main

import "testing"

// TestFreshnessActivationArgumentsFailClosed accepts one canonical activation and rejects six altered version/identity/confirmation/flag cases plus truncated and extra arguments. Only parsing runs; no freshness activation, timer loop or database access occurs.
// TestFreshnessActivationArgumentsFailClosed принимает одну каноническую активацию и отклоняет шесть вариантов версии, идентичности, подтверждения или флага, а также неполные и лишние аргументы. Выполняется только разбор без активации свежести, цикла таймера или доступа к БД.
func TestFreshnessActivationArgumentsFailClosed(t *testing.T) {
	valid := []string{"freshness-activate", "--expected-version", "1", "--activation-id", "13130000-0000-4000-8000-000000000300", "--confirm", "ACTIVATE-STAGING-AUTHORITY-FRESHNESS"}
	action, err := parseCommand(valid)
	if err != nil || action != commandFreshnessActivate {
		t.Fatal("activation command rejected")
	}
	if _, err := parseFreshnessOptions(action, valid); err != nil {
		t.Fatal("activation identity rejected")
	}
	for _, tc := range []struct {
		name  string
		index int
		value string
	}{
		{"future version", 2, "2"}, {"zero identity", 4, "00000000-0000-0000-0000-000000000000"},
		{"noncanonical identity", 4, "13130000000040008000000000000300"},
		{"missing confirmation", 6, ""}, {"production confirmation", 6, "ACTIVATE-PRODUCTION-AUTHORITY-FRESHNESS"},
		{"unknown flag", 1, "--force"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string(nil), valid...)
			args[tc.index] = tc.value
			if _, err := parseFreshnessOptions(action, args); err == nil {
				t.Fatal("invalid activation accepted")
			}
		})
	}
	if _, err := parseCommand(valid[:6]); err == nil {
		t.Fatal("missing argument accepted")
	}
	if _, err := parseCommand(append(valid, "--force")); err == nil {
		t.Fatal("additional argument accepted")
	}
}
