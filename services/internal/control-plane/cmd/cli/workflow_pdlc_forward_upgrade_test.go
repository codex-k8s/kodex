package main

import "testing"

// TestWorkflowPDLCForwardUpgrade exercises the pinned031 upgrade from exact030 with
// real backup SELECT/defaults, native schema proofs, no-op replay and bounded ACL restoration.
// TestWorkflowPDLCForwardUpgrade проверяет закреплённый переход030→031 с настоящими
// backup SELECT/defaults, native schema proofs, no-op replay и ограниченным восстановлением ACL.
func TestWorkflowPDLCForwardUpgrade(t *testing.T) {
	testGitHubForwardUpgrade(t, 20261029000200, 20261029000300, "workflow-pdlc-backup")
}
