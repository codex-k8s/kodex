package sshtarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// TestSSHCredentialConsumerPreservesUnavailableMaterial keeps dependency failures separate from pre-dispatch rejection, clears any partial bytes and never authenticates or executes a command.
// TestSSHCredentialConsumerPreservesUnavailableMaterial сохраняет dependency failures отдельно от pre-dispatch rejection, очищает partial bytes и не начинает authentication либо command.
func TestSSHCredentialConsumerPreservesUnavailableMaterial(t *testing.T) {
	for _, failure := range []error{sshrepository.ErrIdentityUnavailable, errors.New("synthetic private dependency failure"), errors.Join(sshrepository.ErrIdentityUnavailable, sshrepository.ErrIdentityRejected)} {
		locator, reader := credentialFixture(t)
		reader.err = failure
		material, err := LoadCredential(t.Context(), reader, locator)
		if len(material) != 0 || !errors.Is(err, sshrepository.ErrIdentityUnavailable) || errors.Is(err, ErrRejected) || !bytes.Equal(reader.returned, make([]byte, len(reader.returned))) {
			clear(material)
			t.Fatal("unavailable private material became credential rejection or retained bytes")
		}
	}
}

// fixtureSSHMaterialReader owns synthetic ephemeral key bytes and records whether the private SSH-purpose port was reached; no generic credential materializer is present.
// fixtureSSHMaterialReader владеет synthetic ephemeral key bytes и отмечает вызов private SSH-purpose port; generic credential materializer отсутствует.
type fixtureSSHMaterialReader struct {
	material []byte
	returned []byte
	calls    int
	err      error
}

// ReadSSHIdentity returns a distinct owned buffer so rejection-path tests can inspect clearing without mutating the simulated immutable material store.
// ReadSSHIdentity возвращает отдельный owned buffer для проверки clearing на rejection path без изменения simulated immutable material store.
func (reader *fixtureSSHMaterialReader) ReadSSHIdentity(context.Context, value.SSHIdentityLocator) ([]byte, error) {
	reader.calls++
	reader.returned = bytes.Clone(reader.material)
	return reader.returned, reader.err
}

// credentialFixture binds one real ephemeral authentication key to private immutable metadata pins, without publishing the locator or private bytes to any fixture output.
// credentialFixture связывает настоящий ephemeral authentication key с private immutable metadata pins без вывода locator либо private bytes в fixture output.
func credentialFixture(t *testing.T) (value.SSHIdentityLocator, *fixtureSSHMaterialReader) {
	t.Helper()
	signer, private := fixturePrivateKey(t)
	content, public := sha256.Sum256(private), sha256.Sum256(signer.PublicKey().Marshal())
	locator := value.SSHIdentityLocator{Purpose: value.SSHIdentityPurpose, IdentityRevisionRef: "broker_identity_ssh_example01", TargetRef: "ssh_target_example01", Revision: 1, Namespace: "synthetic-ssh", SecretUID: "33333333-3333-4333-8333-333333333333", SecretResourceVersion: "7", ContentSHA256: hex.EncodeToString(content[:]), AuthenticationSHA256: hex.EncodeToString(public[:])}
	locator.SecretName = value.SSHIdentitySecretName(locator.IdentityRevisionRef, locator.Revision)
	return locator, &fixtureSSHMaterialReader{material: private}
}

// TestSSHCredentialPurposeConsumerAuthenticatesWithExactPinnedKey proves the material port feeds the actual encrypted SSH consumer while preserving an owned clearable buffer and a single dispatch.
// TestSSHCredentialPurposeConsumerAuthenticatesWithExactPinnedKey доказывает передачу material port в actual encrypted SSH consumer с owned clearable buffer и единственным dispatch.
func TestSSHCredentialPurposeConsumerAuthenticatesWithExactPinnedKey(t *testing.T) {
	locator, reader := credentialFixture(t)
	prepared, err := PrepareCredential(reader.material)
	if err != nil || prepared.ContentSHA256 != locator.ContentSHA256 || prepared.AuthenticationSHA256 != locator.AuthenticationSHA256 || reader.calls != 0 {
		t.Fatal("broker key preparation changed immutable identity pins")
	}
	material, err := LoadCredential(t.Context(), reader, locator)
	if err != nil || reader.calls != 1 || !bytes.Equal(material, reader.material) {
		t.Fatal("exact SSH-purpose material rejected")
	}
	defer clear(material)
	behavior := &fixtureBehavior{authenticationKey: reader.material, stdout: []byte("safe result")}
	client, target, key := newWireFixture(t, behavior)
	clear(key)
	result, err := client.Run(t.Context(), target, material, Command{Text: "fixture-command", MaximumOutput: 1024})
	if err != nil || !result.SessionExited || string(result.Stdout) != "safe result" || behavior.commands.Load() != 1 {
		t.Fatal("private SSH-purpose consumer did not authenticate exactly once")
	}
	clear(material)
	if !bytes.Equal(reader.material, behavior.authenticationKey) || bytes.Equal(material, reader.material) {
		t.Fatal("material clearing changed immutable source")
	}
}

// TestSSHCredentialPreparationRejectsInvalidMaterialBeforeWrite keeps parsing failures outside materialization without echoing key bytes, passphrase requests or algorithm-specific private diagnostics.
// TestSSHCredentialPreparationRejectsInvalidMaterialBeforeWrite оставляет parsing failures до materialization без key bytes, passphrase requests либо algorithm-specific private diagnostics.
func TestSSHCredentialPreparationRejectsInvalidMaterialBeforeWrite(t *testing.T) {
	for _, material := range [][]byte{nil, []byte("synthetic malformed key"), bytes.Repeat([]byte("x"), 16385)} {
		metadata, err := PrepareCredential(material)
		if !errors.Is(err, ErrRejected) || err.Error() != ErrRejected.Error() || metadata.ContentSHA256 != "" || metadata.AuthenticationSHA256 != "" {
			t.Fatal("invalid material prepared an owner write or leaked diagnostics")
		}
	}
}

// TestSSHCredentialRejectsPurposeAndStalePinsWithoutLeakingMaterial rejects other identity lanes before reading and clears every returned buffer after digest, parser, cancellation or reader failures.
// TestSSHCredentialRejectsPurposeAndStalePinsWithoutLeakingMaterial отклоняет другие identity lanes до чтения и очищает каждый returned buffer после digest, parser, cancellation либо reader failures.
func TestSSHCredentialRejectsPurposeAndStalePinsWithoutLeakingMaterial(t *testing.T) {
	locator, reader := credentialFixture(t)
	wrong := locator
	wrong.Purpose = "KUBERNETES_TARGET"
	if material, err := LoadCredential(t.Context(), reader, wrong); !errors.Is(err, ErrRejected) || len(material) != 0 || reader.calls != 0 {
		t.Fatal("foreign purpose reached SSH material reader")
	}
	for _, change := range []func(*value.SSHIdentityLocator, *fixtureSSHMaterialReader){
		func(locator *value.SSHIdentityLocator, reader *fixtureSSHMaterialReader) {
			locator.ContentSHA256 = strings.Repeat("a", 64)
		},
		func(locator *value.SSHIdentityLocator, reader *fixtureSSHMaterialReader) {
			locator.AuthenticationSHA256 = strings.Repeat("a", 64)
		},
		func(locator *value.SSHIdentityLocator, reader *fixtureSSHMaterialReader) {
			reader.material = []byte("malformed synthetic key")
		},
		func(locator *value.SSHIdentityLocator, reader *fixtureSSHMaterialReader) {
			reader.material = make([]byte, 16385)
		},
		func(locator *value.SSHIdentityLocator, reader *fixtureSSHMaterialReader) {
			reader.err = errors.New("synthetic private SDK diagnostic")
		},
	} {
		locator, reader := credentialFixture(t)
		change(&locator, reader)
		material, err := LoadCredential(t.Context(), reader, locator)
		expected := ErrRejected
		if reader.err != nil {
			expected = sshrepository.ErrIdentityUnavailable
		}
		if !errors.Is(err, expected) || err.Error() != expected.Error() || len(material) != 0 || reader.calls != 1 {
			t.Fatal("invalid SSH material was returned or diagnostic leaked")
		}
		for _, character := range reader.returned {
			if character != 0 {
				t.Fatal("rejected private material was not cleared")
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if material, err := LoadCredential(ctx, reader, locator); !errors.Is(err, sshrepository.ErrIdentityUnavailable) || len(material) != 0 || reader.calls != 0 {
		t.Fatal("cancelled admission reached material reader")
	}
}
