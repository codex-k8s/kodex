package main

import "testing"

// TestWorkflowPDLCForwardUpgrade exercises the stock latest-up command from exact030 with
// real backup SELECT/defaults, native schema proofs, no-op replay and bounded ACL restoration.
// TestWorkflowPDLCForwardUpgrade проверяет штатную latest-up команду от exact030 с настоящими
// backup SELECT/defaults, native schema proofs, no-op replay и ограниченным восстановлением ACL.
func TestWorkflowPDLCForwardUpgrade(t *testing.T) {
	testGitHubForwardUpgrade(t, 20261029000200, 0, "workflow-pdlc-backup")
}
