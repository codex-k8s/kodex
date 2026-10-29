package main

import "testing"

// TestGitHubAutonomousForwardUpgrade applies only the additive GitHub successor to a native028 database with real backup-reader SELECT, then proves once-only replay, exact schema readback, and idempotent ACL restoration.
// TestGitHubAutonomousForwardUpgrade применяет только добавочное обновление GitHub к native028 БД с настоящими backup-reader SELECT, затем проверяет однократность replay, exact schema readback и идемпотентное восстановление ACL.
func TestGitHubAutonomousForwardUpgrade(t *testing.T) {
	testGitHubForwardUpgrade(t, 20261028000100, 20261029000100, "github-autonomous-backup")
}
