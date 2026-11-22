package sshtarget

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/crypto/ssh"
)

// TestDigestHostPinAuthenticatesOnlyExactServer exercises the real SSH handshake with the trusted digest form, including mismatch before authentication and rejection of conflicting or noncanonical pins before dialing.
// TestDigestHostPinAuthenticatesOnlyExactServer проверяет real SSH handshake с trusted digest form, включая mismatch до authentication и отказ conflicting либо noncanonical pins до dial.
func TestDigestHostPinAuthenticatesOnlyExactServer(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		behavior := &fixtureBehavior{command: "synthetic-read", stdout: []byte("bounded-result")}
		client, target, key := newWireFixture(t, behavior)
		defer clear(key)
		public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(target.HostKey))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(public.Marshal())
		target.HostKey = ""
		target.HostKeySHA256 = hex.EncodeToString(digest[:])
		target.HostKeyAlgorithm = managedssh.ED25519HostKey
		if wrong {
			target.HostKeySHA256 = strings.Repeat("0", 64)
		}
		result, err := client.Run(t.Context(), target, key, Command{Text: "synthetic-read", MaximumOutput: 1024})
		if wrong {
			if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || behavior.publicKey.Load() != 0 || behavior.commands.Load() != 0 {
				t.Fatal("digest mismatch crossed authentication boundary")
			}
		} else if err != nil || !result.SessionExited || behavior.publicKey.Load() == 0 || behavior.commands.Load() != 1 {
			t.Fatal("exact host digest rejected real SSH peer")
		}
	}
	for _, target := range []Target{
		{Address: "192.0.2.10", Port: 22, User: "fixture"},
		{Address: "192.0.2.10", Port: 22, User: "fixture", HostKeySHA256: strings.Repeat("A", 64)},
		{Address: "192.0.2.10", Port: 22, User: "fixture", HostKeySHA256: strings.Repeat("a", 63)},
		{Address: "192.0.2.10", Port: 22, User: "fixture", HostKey: "conflicting", HostKeySHA256: strings.Repeat("a", 64)},
	} {
		if target.Validate() == nil {
			t.Fatal("missing, alternate or conflicting host pin accepted")
		}
	}
}

// TestOperatorTargetComesOnlyFromTrustedProfile decodes the actual readonly canonical artifact and materializes its endpoint only for the exact server-selected target, transport mode, key and lifetime; a product caller cannot supply replacement coordinates.
// TestOperatorTargetComesOnlyFromTrustedProfile разбирает actual readonly canonical artifact и материализует endpoint лишь для exact server-selected target, transport mode, key и lifetime; product caller не передаёт replacement coordinates.
func TestOperatorTargetComesOnlyFromTrustedProfile(t *testing.T) {
	file, digest, ref, expected, key, profile := operatorFixture(t)
	defer clear(key)
	read := func(ref string, key []byte, mode managedssh.TransportMode, now int64) (Target, managedssh.TargetQualification, error) {
		if _, err := file.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		return readOperatorTarget(file, digest, ref, key, mode, now)
	}
	target, observed, err := read(ref, key, managedssh.ExecuteTransport, time.Now().Unix())
	if err != nil || target.HostKey != "" || target.HostKeySHA256 != profile.HostKeyDigest || target.Address != expected.Address || target.Port != expected.Port || target.User != expected.User || observed.AuthenticationKey != profile.AuthenticationKey {
		t.Fatal("trusted target projection changed")
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := readOperatorQualification(file, digest, ref, target, key, managedssh.ExecuteTransport, time.Now().Unix()); err != nil {
		t.Fatal("derived target could not rejoin original qualification")
	}
	_, foreignKey := fixturePrivateKey(t)
	defer clear(foreignKey)
	for _, scenario := range []struct {
		ref  string
		key  []byte
		mode managedssh.TransportMode
		now  int64
	}{
		{"foreign-target", key, managedssh.ExecuteTransport, time.Now().Unix()},
		{ref, foreignKey, managedssh.ExecuteTransport, time.Now().Unix()},
		{ref, key, managedssh.ManagedTransport, time.Now().Unix()},
		{ref, key, managedssh.ExecuteTransport, profile.ValidUntil},
	} {
		if _, _, err := read(scenario.ref, scenario.key, scenario.mode, scenario.now); !errors.Is(err, ErrRejected) {
			t.Fatal("foreign target/key/mode or expired profile projected")
		}
	}
}

// TestDigestHostPinSelectsDeclaredKeyAmongMultipleServerKeys proves a trusted RSA pin negotiates only RSA SHA2 when the same server also offers a preferred Ed25519 key; digest-only pinning never guesses the server-selected key type.
// TestDigestHostPinSelectsDeclaredKeyAmongMultipleServerKeys доказывает negotiation только RSA SHA2 для trusted RSA pin также при preferred Ed25519 key того же server; digest-only pinning не угадывает server-selected key type.
func TestDigestHostPinSelectsDeclaredKeyAmongMultipleServerKeys(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal("generate synthetic RSA host key")
	}
	rsaSigner, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal("encode synthetic RSA host key")
	}
	edSigner, _ := fixturePrivateKey(t)
	behavior := &fixtureBehavior{command: "synthetic-read", hostKey: rsaSigner, additionalHostKeys: []ssh.Signer{edSigner}}
	client, target, key := newWireFixture(t, behavior)
	defer clear(key)
	digest := sha256.Sum256(rsaSigner.PublicKey().Marshal())
	target.HostKey = ""
	target.HostKeySHA256 = hex.EncodeToString(digest[:])
	target.HostKeyAlgorithm = managedssh.RSAHostKey
	result, err := client.Run(t.Context(), target, key, Command{Text: behavior.command, MaximumOutput: 1024})
	if err != nil || !result.SessionExited || behavior.commands.Load() != 1 {
		t.Fatal("declared RSA host pin could not select key from multi-key server")
	}
}
