package main

import "testing"

func TestParseCommand(t *testing.T) {
	for _, value := range []command{
		commandExpand,
		commandContract,
		commandUp,
		commandStatus,
		commandVersion,
	} {
		got, err := parseCommand([]string{"migrate", string(value)})
		if err != nil {
			t.Fatalf("parseCommand(%q) error = %v", value, err)
		}
		if got != value {
			t.Fatalf("parseCommand(%q) = %q", value, got)
		}
	}
}

func TestParseCommandRejectsLegacyAndUnknownCommands(t *testing.T) {
	for _, arguments := range [][]string{
		{"migrate"},
		{"status"},
		{"migrate", "down"},
		{"migrate", "expand", "extra"},
	} {
		if _, err := parseCommand(arguments); err == nil {
			t.Fatalf("parseCommand(%q) accepted invalid arguments", arguments)
		}
	}
}
