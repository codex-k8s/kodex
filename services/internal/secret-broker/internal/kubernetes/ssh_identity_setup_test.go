package kubernetes

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	"golang.org/x/crypto/ssh"
)

// sshSetupWireOwner represents the private owner RPC boundary for the actual parser/TLS/storage consumer test; it is not a PostgreSQL qualification or an alternate production owner.
// sshSetupWireOwner представляет private owner RPC boundary для actual parser/TLS/storage consumer test; это не PostgreSQL qualification либо alternate production owner.
type sshSetupWireOwner struct {
	snapshot                                    value.SSHIdentityUploadSnapshot
	permitSHA256                                string
	attempted, eligible, closed, dropCompletion bool
	claims, completions                         int
	last                                        sshrepository.IdentityUploadCompletion
}

// ResolveSSHIdentityUpload preserves owner-side refusal after an unreported or rejected write while allowing only explicitly committed transport ambiguity to be observed.
// ResolveSSHIdentityUpload сохраняет owner-side refusal после unreported либо rejected write и допускает observation только явно committed transport ambiguity.
func (owner *sshSetupWireOwner) ResolveSSHIdentityUpload(_ context.Context, ref, permit string) (value.SSHIdentityUploadSnapshot, error) {
	if ref != owner.snapshot.SetupRef || permit != owner.permitSHA256 || owner.closed || owner.attempted && !owner.eligible {
		return value.SSHIdentityUploadSnapshot{}, errors.New("synthetic setup is closed")
	}
	snapshot := owner.snapshot
	if owner.attempted {
		snapshot.Mode = value.SSHIdentityUploadObserve
	}
	return snapshot, nil
}

// ClaimSSHIdentityUpload stores one sole-write decision and the broker-derived public key pin before returning a short exact fence.
// ClaimSSHIdentityUpload сохраняет sole-write decision и broker-derived public key pin до возврата short exact fence.
func (owner *sshSetupWireOwner) ClaimSSHIdentityUpload(_ context.Context, input sshrepository.IdentityUploadClaim) (value.SSHIdentityUploadAdmission, error) {
	owner.claims++
	mode := value.SSHIdentityUploadWrite
	if owner.attempted {
		mode = value.SSHIdentityUploadObserve
	}
	owner.attempted = true
	owner.snapshot.AuthenticationSHA256 = input.Key.AuthenticationSHA256
	provision, valid := owner.snapshot.Preparation.Provision(input.Key)
	if !valid {
		return value.SSHIdentityUploadAdmission{}, errors.New("synthetic setup key changed")
	}
	return value.SSHIdentityUploadAdmission{SetupRef: input.SetupRef, SnapshotDigest: input.SnapshotDigest, AttemptRef: "ssa_actual_tls_fixture", WorkloadInstance: input.WorkloadInstance, Fence: "fence_actual_tls_fixture", Generation: int64(owner.claims), Mode: mode, Provision: provision, ExpiresAt: time.Now().Add(20 * time.Second)}, nil
}

// CompleteSSHIdentityUpload records the classified receipt once; a simulated loss before its transaction never grants read-recovery credit.
// CompleteSSHIdentityUpload сохраняет classified receipt один раз; simulated loss до transaction не даёт read-recovery credit.
func (owner *sshSetupWireOwner) CompleteSSHIdentityUpload(_ context.Context, completion sshrepository.IdentityUploadCompletion) error {
	owner.completions++
	if owner.dropCompletion {
		return errors.New("synthetic owner completion lost")
	}
	if !completion.Valid() {
		return errors.New("synthetic completion malformed")
	}
	owner.last = completion
	owner.eligible = completion.Unknown
	owner.closed = completion.Rejected != nil || completion.Identity != nil
	return nil
}

// TestSSHIdentitySetupRealParserAndTLSStorage composes real key parsing, the domain workflow and actual TLS client-go requests, separating lost transport ACK recovery from terminal damaged success and unreported completion.
// TestSSHIdentitySetupRealParserAndTLSStorage связывает real key parsing, domain workflow и actual TLS client-go requests, разделяя lost transport ACK recovery, terminal damaged success и unreported completion.
func TestSSHIdentitySetupRealParserAndTLSStorage(t *testing.T) {
	for _, mode := range []string{"known", "lost ACK", "bad ACK", "unreported bad ACK"} {
		t.Run(mode, func(t *testing.T) {
			store, wire, _ := newSSHMaterialWire(t)
			_, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal("generate synthetic SSH key")
			}
			defer clear(key)
			block, err := ssh.MarshalPrivateKey(key, "")
			if err != nil {
				t.Fatal("encode synthetic SSH key")
			}
			material := pem.EncodeToMemory(block)
			defer clear(material)
			metadata, err := sshtarget.PrepareCredential(material)
			if err != nil {
				t.Fatal("parse actual SSH key")
			}
			wire.provision.ContentSHA256, wire.provision.AuthenticationSHA256 = metadata.ContentSHA256, metadata.AuthenticationSHA256
			permit := bytes.Repeat([]byte{7}, 32)
			permitSHA := sha256.Sum256(permit)
			owner := &sshSetupWireOwner{permitSHA256: hex.EncodeToString(permitSHA[:]), snapshot: value.SSHIdentityUploadSnapshot{SetupRef: "ssu_actual_tls_fixture", Digest: strings.Repeat("a", 64), Mode: value.SSHIdentityUploadWrite, Preparation: value.SSHIdentityPreparation{IdentityRevisionRef: wire.provision.IdentityRevisionRef, TargetRef: wire.provision.TargetRef, Namespace: wire.provision.Namespace, Revision: wire.provision.Revision, ContentSHA256: metadata.ContentSHA256}, ContentBytes: uint32(len(material)), ExpiresAt: time.Now().Add(time.Minute)}}
			workflow, err := sshidentity.New(owner, sshtarget.KeyPreparer{}, store, "pod_initial_fixture", wire.provision.Namespace)
			if err != nil {
				t.Fatal("compose actual TLS setup")
			}
			wire.createDrop = mode == "lost ACK"
			if strings.Contains(mode, "bad ACK") {
				wire.createACK = "wrong tuple"
			}
			owner.dropCompletion = mode == "unreported bad ACK"
			err = workflow.Materialize(t.Context(), owner.snapshot.SetupRef, permit, material)
			if (mode == "known") != (err == nil) || wire.posts.Load() != 1 || wire.gets.Load() != 0 || owner.claims != 1 || owner.completions != 1 {
				t.Fatal("actual workflow violated single materialization/receipt boundary")
			}
			if mode == "bad ACK" && (owner.last.Rejected == nil || owner.last.Rejected.ObservedUID != "wire-uid-synthetic") {
				t.Fatal("acknowledged original UID was not retained in terminal owner fact")
			}
			// The new handler has no prior in-memory error. Only owner state can admit its recovery.
			// Новый handler не имеет прежней in-memory error. Его recovery разрешает только owner state.
			fresh, err := sshidentity.New(owner, sshtarget.KeyPreparer{}, store, "pod_replacement_fixture", wire.provision.Namespace)
			if err != nil {
				t.Fatal("compose replacement handler")
			}
			if strings.Contains(mode, "bad ACK") {
				wire.mutex.Lock()
				wire.secret.UID = "replacement-must-not-be-adopted"
				wire.mutex.Unlock()
			}
			err = fresh.Observe(t.Context(), owner.snapshot.SetupRef, permit)
			if (mode == "lost ACK") != (err == nil) || (mode == "lost ACK") != (wire.gets.Load() == 1) || wire.posts.Load() != 1 {
				t.Fatal("damaged or unreported ACK acquired replacement observation credit")
			}
			if mode == "known" || mode == "lost ACK" {
				if owner.last.Identity == nil {
					t.Fatal("known exact material receipt missing")
				}
				private, err := sshtarget.LoadCredential(t.Context(), store, *owner.last.Identity)
				if err != nil || !bytes.Equal(private, material) {
					clear(private)
					t.Fatal("bound actual material failed private runtime consumer")
				}
				clear(private)
			}
			if fresh.Materialize(t.Context(), owner.snapshot.SetupRef, permit, material) == nil || wire.posts.Load() != 1 {
				t.Fatal("replacement handler repeated original material write")
			}
		})
	}
}
