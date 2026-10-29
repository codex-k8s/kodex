package main

import "testing"

// TestGitHubPRForwardUpgrade applies only the additive PR collaboration successor to a native029 database with real backup-reader SELECT, then proves once-only replay, exact schema readback, and idempotent ACL restoration.
// TestGitHubPRForwardUpgrade применяет только добавочное обновление GitHub к native029 БД с настоящими backup-reader SELECT, затем проверяет однократность replay, exact schema readback и идемпотентное восстановление ACL.
func TestGitHubPRForwardUpgrade(t *testing.T) {
	testGitHubForwardUpgrade(t, 20261029000100, 0, "github-pr-backup")
}
