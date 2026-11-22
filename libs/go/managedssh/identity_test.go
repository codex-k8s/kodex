package managedssh

import (
	"math"
	"strings"
	"testing"
)

// TestPrivateIdentityContractPreservesStoredObjectPins checks the already-used private object name and opaque UID/resourceVersion contract so moving it across owner/broker modules cannot rename or reinterpret retained material.
// TestPrivateIdentityContractPreservesStoredObjectPins проверяет уже используемые private object name и opaque UID/resourceVersion contract, чтобы перенос между owner/broker modules не переименовал и не переинтерпретировал retained material.
func TestPrivateIdentityContractPreservesStoredObjectPins(t *testing.T) {
	preparation := IdentityPreparation{IdentityRevisionRef: "broker_identity_ssh_example01", TargetRef: "ssh_target_example01", Revision: 1, Namespace: "synthetic-ssh", ContentSHA256: strings.Repeat("a", 64)}
	key := IdentityKeyMetadata{ContentSHA256: preparation.ContentSHA256, AuthenticationSHA256: strings.Repeat("b", 64)}
	provision, valid := preparation.Provision(key)
	if !preparation.Valid() || !valid || provision.Purpose != IdentityPurpose {
		t.Fatal("parsed key cannot complete original purpose preparation")
	}
	locator := IdentityLocator{Purpose: provision.Purpose, IdentityRevisionRef: provision.IdentityRevisionRef, TargetRef: provision.TargetRef, Revision: provision.Revision, Namespace: provision.Namespace, SecretName: IdentitySecretName(provision.IdentityRevisionRef, provision.Revision), SecretUID: "opaque-nonUUID-v1", SecretResourceVersion: "opaque-nonnumeric-rv", ContentSHA256: provision.ContentSHA256, AuthenticationSHA256: provision.AuthenticationSHA256}
	if !locator.Valid() || locator.Provision() != provision || locator.SecretName != "sshbi-7db8468992942b1c2a712b383de00577-r1" {
		t.Fatal("shared predicate changed retained identity coordinates")
	}
	for _, change := range []func(*IdentityLocator){
		func(l *IdentityLocator) { l.Purpose = "TOKEN" },
		func(l *IdentityLocator) { l.Purpose = "KUBERNETES_TARGET" },
		func(l *IdentityLocator) { l.Namespace = "../synthetic-ssh" },
		func(l *IdentityLocator) { l.IdentityRevisionRef = "foreign_revision" },
		func(l *IdentityLocator) { l.Revision++ },
		func(l *IdentityLocator) { l.SecretUID = "" },
		func(l *IdentityLocator) { l.SecretResourceVersion = "" },
		func(l *IdentityLocator) { l.AuthenticationSHA256 = "" },
	} {
		broken := locator
		change(&broken)
		if broken.Valid() {
			t.Fatal("foreign or incomplete private identity admitted")
		}
	}
	if name := IdentitySecretName(preparation.IdentityRevisionRef, math.MaxInt64); len(name) > 63 || name == "" {
		t.Fatal("highest immutable revision lost bounded object naming")
	}
}

// TestPendingIdentityNeverInventsAuthenticationOrScope rejects missing actual key metadata and alternate namespace spellings before the shared contract can enter a private owner claim.
// TestPendingIdentityNeverInventsAuthenticationOrScope отклоняет missing actual key metadata и alternate namespace spellings до передачи общего contract private owner claim.
func TestPendingIdentityNeverInventsAuthenticationOrScope(t *testing.T) {
	preparation := IdentityPreparation{IdentityRevisionRef: "broker_identity_ssh_example01", TargetRef: "ssh_target_example01", Revision: 1, Namespace: "synthetic-ssh", ContentSHA256: strings.Repeat("a", 64)}
	for _, key := range []IdentityKeyMetadata{{}, {ContentSHA256: preparation.ContentSHA256}, {ContentSHA256: strings.Repeat("c", 64), AuthenticationSHA256: strings.Repeat("b", 64)}} {
		if _, valid := preparation.Provision(key); valid {
			t.Fatal("pending identity invented actual key evidence")
		}
	}
	for _, namespace := range []string{"", "../synthetic-ssh", "team/name", "UPPER", "team.space", strings.Repeat("a", 64), "-leading", "trailing-"} {
		if IdentityNamespace(namespace) {
			t.Fatal("alternate private namespace spelling admitted")
		}
	}
	for _, namespace := range []string{"a", "synthetic-ssh", strings.Repeat("a", 63)} {
		if !IdentityNamespace(namespace) {
			t.Fatal("canonical private namespace rejected")
		}
	}
}
