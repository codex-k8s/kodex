package main

import "testing"

// TestSTTTransportPermissionDoesNotUseDomainAlias preserves separate transport and catalog permissions.
// TestSTTTransportPermissionDoesNotUseDomainAlias сохраняет разные права транспорта и каталога.
func TestSTTTransportPermissionDoesNotUseDomainAlias(t *testing.T) {
	if permissionForOperation("platform.stt.transcribe") != "platform.stt.transcribe" {
		t.Fatal("STT transport permission was replaced by a domain alias")
	}
	if permissionForOperation("platform.stt.model-catalog.get") != "system.configuration.manage" {
		t.Fatal("STT catalog permission changed")
	}
}
