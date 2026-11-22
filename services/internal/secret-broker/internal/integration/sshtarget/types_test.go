package sshtarget

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/crypto/ssh"
)

// TestTargetRejectsImplicitDestinationsAndPrivilegedNames covers canonical literal network/account bounds before any credential authentication.
// TestTargetRejectsImplicitDestinationsAndPrivilegedNames проверяет canonical literal network/account bounds до credential authentication.
func TestTargetRejectsImplicitDestinationsAndPrivilegedNames(t *testing.T) {
	signer, _ := fixturePrivateKey(t)
	baseline := Target{Address: "192.0.2.10", Port: 22, User: "fixture", HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))}
	for _, address := range []string{"192.0.2.10", "10.0.0.10", "2001:db8::10", "fd00::10"} {
		target := baseline
		target.Address = address
		if target.Validate() != nil {
			t.Fatal("explicit routed literal target rejected")
		}
	}
	for _, address := range []string{"example.invalid", "127.0.0.1", "::1", "::", "0.0.0.0", "169.254.169.254", "fe80::1", "fe80::1%eth0", "224.0.0.1", "::ffff:192.0.2.10", "2001:0db8::10", "192.0.2.10:22"} {
		target := baseline
		target.Address = address
		if target.Validate() == nil {
			t.Fatal("implicit or unsafe target accepted")
		}
	}
	for _, user := range []string{"", "root", "-option", "user name", "user@host", "user;id", "user\n", strings.Repeat("a", 33)} {
		target := baseline
		target.User = user
		if target.Validate() == nil {
			t.Fatal("ambiguous or privileged account name accepted")
		}
	}
}

// TestRunRejectsInvalidInputsBeforeDial ensures malformed key pins, options, command/input bounds and absent keys cannot begin an external attempt.
// TestRunRejectsInvalidInputsBeforeDial проверяет, что malformed key pins, options, command/input bounds и отсутствие keys не начинают внешнюю attempt.
func TestRunRejectsInvalidInputsBeforeDial(t *testing.T) {
	signer, private := fixturePrivateKey(t)
	defer clear(private)
	target := Target{Address: "192.0.2.10", Port: 22, User: "fixture", HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))}
	command := Command{Text: "sensitive-command", MaximumOutput: 32768}
	dialed := false
	client := Client{dial: func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("private endpoint or diagnostic")
	}}
	for _, pin := range []string{"not-a-key", target.HostKey + " comment", target.HostKey + "\n", "restrict " + target.HostKey, target.HostKey + "\n" + target.HostKey} {
		invalid := target
		invalid.HostKey = pin
		if _, err := client.Run(t.Context(), invalid, private, command); !errors.Is(err, ErrRejected) || dialed {
			t.Fatal("invalid host pin crossed dial boundary")
		}
	}
	for _, invalid := range []Command{{Text: "", MaximumOutput: 1}, {Text: "bad\x00command", MaximumOutput: 1}, {Text: "command", MaximumOutput: 0}, {Text: "command", MaximumOutput: 32769}, {Text: strings.Repeat("x", 8193), MaximumOutput: 1}, {Text: "command", Input: make([]byte, 65537), MaximumOutput: 1}} {
		if _, err := client.Run(t.Context(), target, private, invalid); !errors.Is(err, ErrRejected) || dialed {
			t.Fatal("invalid command crossed dial boundary")
		}
	}
	if _, err := client.Run(t.Context(), target, make([]byte, 16385), command); !errors.Is(err, ErrRejected) || dialed {
		t.Fatal("credential exceeded the shared UI and owner 16 KiB budget")
	}
	_, err := client.Run(t.Context(), target, private, command)
	if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || err.Error() != ErrRejected.Error() || !dialed {
		t.Fatal("pre-dispatch failure lost fixed diagnostic")
	}
}

// TestHostKeysPermitModernRawAlgorithmsOnly qualifies the explicitly supported algorithm set and rejects weak RSA and certificate-based implicit authorities.
// TestHostKeysPermitModernRawAlgorithmsOnly проверяет явно поддержанный набор algorithms и отклоняет слабый RSA и certificate-based implicit authorities.
func TestHostKeysPermitModernRawAlgorithmsOnly(t *testing.T) {
	for _, curve := range []elliptic.Curve{elliptic.P256(), elliptic.P384(), elliptic.P521()} {
		private, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			t.Fatal("generate isolated ECDSA key")
		}
		signer, err := ssh.NewSignerFromKey(private)
		if err != nil || len(hostKeyAlgorithms(signer.PublicKey())) != 1 {
			t.Fatal("modern ECDSA host key rejected")
		}
	}
	for _, bits := range []int{2048, 3072} {
		private, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			t.Fatal("generate isolated RSA key")
		}
		signer, err := ssh.NewSignerFromKey(private)
		if err != nil {
			t.Fatal("create isolated RSA signer")
		}
		algorithms := hostKeyAlgorithms(signer.PublicKey())
		digest := sha256.Sum256(signer.PublicKey().Marshal())
		pin, pinErr := (Target{HostKeySHA256: hex.EncodeToString(digest[:]), HostKeyAlgorithm: managedssh.RSAHostKey}).hostPin()
		if pinErr != nil || pin.matches(signer.PublicKey()) != (bits >= 3072) {
			t.Fatal("digest pin bypassed RSA key strength boundary")
		}
		if bits == 2048 && len(algorithms) != 0 || bits == 3072 && (len(algorithms) != 2 || algorithms[0] != ssh.KeyAlgoRSASHA512 || algorithms[1] != ssh.KeyAlgoRSASHA256) {
			t.Fatal("RSA SHA-2 or minimum size policy changed")
		}
		block, err := ssh.MarshalPrivateKey(private, "")
		if err != nil {
			t.Fatal("encode isolated RSA key")
		}
		raw := pem.EncodeToMemory(block)
		restricted, restrictionErr := authenticationSigner(raw)
		clear(raw)
		if bits == 2048 {
			if restrictionErr == nil {
				t.Fatal("weak RSA authentication key accepted")
			}
		} else if restrictionErr != nil || !reflect.DeepEqual(restricted.(ssh.MultiAlgorithmSigner).Algorithms(), []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}) {
			t.Fatal("RSA authentication can fall back to legacy signatures")
		}
	}
	signer, _ := fixturePrivateKey(t)
	certificate := &ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.HostCert, ValidBefore: ssh.CertTimeInfinity}
	if err := certificate.SignCert(rand.Reader, signer); err != nil {
		t.Fatal("sign synthetic certificate")
	}
	certificateDigest := sha256.Sum256(certificate.Marshal())
	certificatePin, pinErr := (Target{HostKeySHA256: hex.EncodeToString(certificateDigest[:]), HostKeyAlgorithm: managedssh.ED25519HostKey}).hostPin()
	if pinErr != nil || certificatePin.matches(certificate) {
		t.Fatal("digest pin introduced certificate authority")
	}
	if len(hostKeyAlgorithms(&ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.HostCert, ValidBefore: ssh.CertTimeInfinity})) != 0 {
		t.Fatal("certificate introduced implicit host authority")
	}
}

// TestFailureKeepsUnknownAuthoritativeOverItsPrivateCause prevents cause inspection from turning a post-dispatch response rejection into permission to resend.
// TestFailureKeepsUnknownAuthoritativeOverItsPrivateCause не позволяет cause inspection превратить response rejection после dispatch в разрешение повтора.
func TestFailureKeepsUnknownAuthoritativeOverItsPrivateCause(t *testing.T) {
	failure := &Failure{unknown: true, cause: ErrRejected}
	if !errors.Is(failure, ErrUnknown) || errors.Is(failure, ErrRejected) || failure.Error() != ErrUnknown.Error() {
		t.Fatal("private cause overrode authoritative unknown outcome")
	}
	native := &net.OpError{Op: "private-operation", Err: context.DeadlineExceeded}
	failure.cause = native
	var classified *net.OpError
	if !errors.Is(failure, context.DeadlineExceeded) || !errors.As(failure, &classified) || classified != native || strings.Contains(failure.Error(), native.Op) {
		t.Fatal("safe failure lost private typed classification")
	}
}
