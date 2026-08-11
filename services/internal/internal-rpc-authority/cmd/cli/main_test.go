package main

import "testing"

func TestParseCommandAcceptsDeploy(t *testing.T) {
	t.Parallel()

	action, err := parseCommand([]string{"migrate", "deploy"})
	if err != nil {
		t.Fatalf("parse deploy command: %v", err)
	}
	if action != commandDeploy {
		t.Fatalf("unexpected deploy command: %q", action)
	}
}
