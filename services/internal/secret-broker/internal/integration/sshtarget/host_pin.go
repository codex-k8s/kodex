package sshtarget

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/crypto/ssh"
)

// hostPin retains one exact trusted key representation; digest-only pins come from the deployment-owned operator artifact and still reject weak or certificate keys before user authentication.
// hostPin сохраняет exact trusted key representation; digest-only pins берутся из deployment-owned operator artifact и отвергают weak либо certificate keys до user authentication.
type hostPin struct {
	digest     [sha256.Size]byte
	public     ssh.PublicKey
	algorithm  managedssh.HostKeyAlgorithm
	algorithms []string
}

// hostPin resolves exactly one explicit public key or canonical SHA256 pin without network access, inference from a peer, hostname trust or TOFU.
// hostPin разрешает ровно один explicit public key либо canonical SHA256 pin без network access, вывода из peer, hostname trust либо TOFU.
func (target Target) hostPin() (hostPin, error) {
	if target.HostKey != "" {
		if target.HostKeySHA256 != "" || target.HostKeyAlgorithm != "" {
			return hostPin{}, ErrRejected
		}
		public, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(target.HostKey))
		if err != nil || len(options) != 0 || len(rest) != 0 || strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))) != target.HostKey {
			return hostPin{}, ErrRejected
		}
		algorithms := hostKeyAlgorithms(public)
		if len(algorithms) == 0 {
			return hostPin{}, ErrRejected
		}
		return hostPin{digest: sha256.Sum256(public.Marshal()), public: public, algorithm: managedssh.HostKeyAlgorithm(public.Type()), algorithms: algorithms}, nil
	}
	if !managedssh.Digest(target.HostKeySHA256) || !target.HostKeyAlgorithm.Valid() {
		return hostPin{}, ErrRejected
	}
	raw, err := hex.DecodeString(target.HostKeySHA256)
	if err != nil || len(raw) != sha256.Size {
		return hostPin{}, ErrRejected
	}
	var digest [sha256.Size]byte
	copy(digest[:], raw)
	algorithms := []string{string(target.HostKeyAlgorithm)}
	if target.HostKeyAlgorithm == managedssh.RSAHostKey {
		algorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return hostPin{digest: digest, algorithm: target.HostKeyAlgorithm, algorithms: algorithms}, nil
}

// matches checks modern key type/size and exact trusted bytes or SHA256 before authentication; a matching digest never admits a weak RSA, SHA1 signature or an implicit certificate authority.
// matches проверяет modern key type/size и exact trusted bytes либо SHA256 до authentication; matching digest не допускает weak RSA, SHA1 signature либо implicit certificate authority.
func (pin hostPin) matches(presented ssh.PublicKey) bool {
	if presented == nil || len(hostKeyAlgorithms(presented)) == 0 || managedssh.HostKeyAlgorithm(presented.Type()) != pin.algorithm {
		return false
	}
	digest := sha256.Sum256(presented.Marshal())
	if subtle.ConstantTimeCompare(pin.digest[:], digest[:]) != 1 {
		return false
	}
	return pin.public == nil || subtle.ConstantTimeCompare(pin.public.Marshal(), presented.Marshal()) == 1
}
