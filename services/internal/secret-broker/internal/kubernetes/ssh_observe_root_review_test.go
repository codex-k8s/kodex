package kubernetes

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	"golang.org/x/crypto/ssh"
	"strings"
	"testing"
	"time"
)

// TestRootSSHObserveDamagedSuccessStopsRecovery checks the actual TLS store and service after a lost CREATE response: a completed mismatched GET must not authorize adopting a later replacement.
// TestRootSSHObserveDamagedSuccessStopsRecovery проверяет actual TLS store и service после потерянного CREATE response: завершённый mismatched GET не разрешает принять позднюю замену.
func TestRootSSHObserveDamagedSuccessStopsRecovery(t *testing.T) {
	store, wire, _ := newSSHMaterialWire(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic key")
	}
	defer clear(key)
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal("marshal synthetic key")
	}
	material := pem.EncodeToMemory(block)
	defer clear(material)
	metadata, err := sshtarget.PrepareCredential(material)
	if err != nil {
		t.Fatal("parse synthetic key")
	}
	wire.provision.ContentSHA256, wire.provision.AuthenticationSHA256 = metadata.ContentSHA256, metadata.AuthenticationSHA256
	permit := bytes.Repeat([]byte{7}, 32)
	defer clear(permit)
	permitSHA := sha256.Sum256(permit)
	owner := &sshSetupWireOwner{permitSHA256: hex.EncodeToString(permitSHA[:]), snapshot: value.SSHIdentityUploadSnapshot{SetupRef: "ssu_root_tls_review", Digest: strings.Repeat("a", 64), Mode: value.SSHIdentityUploadWrite, Preparation: value.SSHIdentityPreparation{IdentityRevisionRef: wire.provision.IdentityRevisionRef, TargetRef: wire.provision.TargetRef, Namespace: wire.provision.Namespace, Revision: wire.provision.Revision, ContentSHA256: metadata.ContentSHA256}, ContentBytes: uint32(len(material)), ExpiresAt: time.Now().Add(time.Minute)}}
	workflow, err := sshidentity.New(owner, sshtarget.KeyPreparer{}, store, "pod_root_tls_review", wire.provision.Namespace)
	if err != nil {
		t.Fatal("compose synthetic workflow")
	}
	wire.createDrop = true
	if err := workflow.Materialize(t.Context(), owner.snapshot.SetupRef, permit, material); !errors.Is(err, sshrepository.ErrIdentityUnknown) || !owner.eligible {
		t.Fatal("initial lost acknowledgement did not retain UNKNOWN")
	}
	wire.mutex.Lock()
	wire.secret.Annotations[sshIdentityTargetAnnotation] = "foreign-target"
	wire.mutex.Unlock()
	err = workflow.Observe(t.Context(), owner.snapshot.SetupRef, permit)
	if !errors.Is(err, sshrepository.ErrIdentityAcknowledgementRejected) || owner.last.Rejected == nil || owner.last.Unknown || !owner.closed {
		t.Error("completed damaged GET was classified as recoverable UNKNOWN instead of terminal rejection")
	}
	wire.mutex.Lock()
	wire.secret.Annotations[sshIdentityTargetAnnotation] = wire.provision.TargetRef
	wire.secret.UID = "replacement-after-damaged-observation"
	wire.mutex.Unlock()
	err = workflow.Observe(t.Context(), owner.snapshot.SetupRef, permit)
	if err == nil || owner.last.Identity != nil || wire.gets.Load() != 1 {
		t.Error("later replacement was adopted after damaged successful observation")
	}
	if wire.posts.Load() != 1 {
		t.Error("original CREATE was repeated")
	}
}
