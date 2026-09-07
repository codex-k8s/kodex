package main

import "testing"

func TestSTTTransportPermissionDoesNotUseDomainAlias(t *testing.T) {
	if permissionForOperation("platform.stt.transcribe") != "platform.stt.transcribe" {
		t.Fatal("STT transport permission was replaced by a domain alias")
	}
	if permissionForOperation("platform.stt.model-catalog.get") != "system.configuration.manage" {
		t.Fatal("STT catalog permission changed")
	}
}
