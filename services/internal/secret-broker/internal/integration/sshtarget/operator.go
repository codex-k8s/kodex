package sshtarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// OperatorQualificationFile is the fixed deployment-owned read-only artifact; neither a connection field nor an employee request may choose its path or expected digest.
// OperatorQualificationFile задаёт fixed deployment-owned read-only artifact; connection field либо employee request не могут выбирать path либо expected digest.
const OperatorQualificationFile = managedssh.OperatorQualificationFile

// OperatorQualifications stores only trusted deployment pins, never credential bytes; every operation rereads the current artifact to observe revocation and expiry.
// OperatorQualifications хранит только trusted deployment pins без credential bytes; каждая operation перечитывает текущий artifact для проверки revocation и expiry.
type OperatorQualifications struct {
	digest string
}

// NewOperatorQualifications accepts an optional exact immutable mount and content digest; an unconfigured deployment remains unavailable rather than inferring qualification from connection JSON.
// NewOperatorQualifications принимает optional exact immutable mount и content digest; ненастроенный deployment остаётся unavailable без вывода qualification из connection JSON.
func NewOperatorQualifications(file, digest string) (OperatorQualifications, error) {
	if file == "" && digest == "" {
		return OperatorQualifications{}, nil
	}
	if file != OperatorQualificationFile || !managedssh.Digest(digest) {
		return OperatorQualifications{}, ErrRejected
	}
	return OperatorQualifications{digest: digest}, nil
}

// Resolve binds the current server-owned target and broker-held authentication key to a fresh operator artifact before SSH; it does not replace owner permission, current generation, consent or lease checks.
// Resolve связывает current server-owned target и broker-held authentication key со fresh operator artifact до SSH; owner permission, current generation, consent либо lease checks он не заменяет.
func (operator OperatorQualifications) Resolve(targetRef string, target Target, privateKey []byte, mode managedssh.TransportMode) (managedssh.TargetQualification, error) {
	if !managedssh.Digest(operator.digest) || target.Validate() != nil || len(privateKey) == 0 || len(privateKey) > 16384 {
		return managedssh.TargetQualification{}, ErrRejected
	}
	file, err := os.Open(OperatorQualificationFile)
	if err != nil {
		return managedssh.TargetQualification{}, ErrRejected
	}
	defer file.Close()
	return readOperatorQualification(file, operator.digest, targetRef, target, privateKey, mode, time.Now().Unix())
}

// readOperatorQualification consumes one bounded regular read-only inode, checks its complete immutable digest and selects exact target, mode and both SSH public keys without echoing private parser errors.
// readOperatorQualification читает один bounded regular read-only inode, проверяет полный immutable digest и выбирает exact target, mode и оба SSH public keys без вывода private parser errors.
func readOperatorQualification(file *os.File, expected, targetRef string, target Target, privateKey []byte, mode managedssh.TransportMode, now int64) (managedssh.TargetQualification, error) {
	if target.Validate() != nil || len(privateKey) == 0 || len(privateKey) > 16384 {
		return managedssh.TargetQualification{}, ErrRejected
	}
	document, err := readOperatorQualifications(file, expected, now)
	if err != nil {
		return managedssh.TargetQualification{}, ErrRejected
	}
	pin, err := target.hostPin()
	if err != nil {
		return managedssh.TargetQualification{}, ErrRejected
	}
	signer, err := authenticationSigner(privateKey)
	if err != nil {
		return managedssh.TargetQualification{}, ErrRejected
	}
	keyDigest := sha256.Sum256(signer.PublicKey().Marshal())
	for _, profile := range document.Profiles {
		if profile.TargetRef == targetRef && profile.Address == target.Address && profile.Port == target.Port && profile.User == target.User && profile.Mode == mode && profile.HostKeyDigest == hex.EncodeToString(pin.digest[:]) && profile.HostKeyAlgorithm == pin.algorithm && profile.AuthenticationKey == hex.EncodeToString(keyDigest[:]) {
			return profile, nil
		}
	}
	return managedssh.TargetQualification{}, ErrRejected
}

// readOperatorQualifications validates the full bounded immutable deployment document before selecting any profile, preserving rejection of unrelated malformed or expired entries.
// readOperatorQualifications проверяет полный bounded immutable deployment document до выбора profile, сохраняя отказ при посторонних malformed либо expired entries.
func readOperatorQualifications(file *os.File, expected string, now int64) (managedssh.Qualifications, error) {
	if file == nil {
		return managedssh.Qualifications{}, ErrRejected
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0222 != 0 || before.Size() < 1 || before.Size() > managedssh.MaximumFrame || !managedssh.Digest(expected) {
		return managedssh.Qualifications{}, ErrRejected
	}
	raw, err := io.ReadAll(io.LimitReader(file, managedssh.MaximumFrame+1))
	after, final := file.Stat()
	if err != nil || final != nil || len(raw) != int(before.Size()) || len(raw) > managedssh.MaximumFrame || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		return managedssh.Qualifications{}, ErrRejected
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expected {
		return managedssh.Qualifications{}, ErrRejected
	}
	document, err := managedssh.DecodeQualifications(raw, now)
	if err != nil {
		return managedssh.Qualifications{}, ErrRejected
	}
	return document, nil
}

// ResolveTarget materializes endpoint/account/host-key pins solely from current trusted operator evidence and the broker-held authentication key; no product configuration can select a destination or learn key bytes.
// ResolveTarget материализует endpoint/account/host-key pins только из current trusted operator evidence и broker-held authentication key; product configuration не выбирает destination и не получает key bytes.
func (operator OperatorQualifications) ResolveTarget(targetRef string, privateKey []byte, mode managedssh.TransportMode) (Target, managedssh.TargetQualification, error) {
	if !managedssh.Digest(operator.digest) {
		return Target{}, managedssh.TargetQualification{}, ErrRejected
	}
	file, err := os.Open(OperatorQualificationFile)
	if err != nil {
		return Target{}, managedssh.TargetQualification{}, ErrRejected
	}
	defer file.Close()
	return readOperatorTarget(file, operator.digest, targetRef, privateKey, mode, time.Now().Unix())
}

// readOperatorTarget binds a server-selected ref and actual parsed authentication key to exactly one profile, deriving the host pin without TOFU or a second endpoint registry.
// readOperatorTarget связывает server-selected ref и actual parsed authentication key ровно с одним profile, выводя host pin без TOFU либо второго endpoint registry.
func readOperatorTarget(file *os.File, expected, targetRef string, privateKey []byte, mode managedssh.TransportMode, now int64) (Target, managedssh.TargetQualification, error) {
	if !managedssh.OwnerReference(targetRef) || len(privateKey) == 0 || len(privateKey) > 16384 || mode != managedssh.ManagedTransport && mode != managedssh.ExecuteTransport {
		return Target{}, managedssh.TargetQualification{}, ErrRejected
	}
	document, err := readOperatorQualifications(file, expected, now)
	if err != nil {
		return Target{}, managedssh.TargetQualification{}, ErrRejected
	}
	signer, err := authenticationSigner(privateKey)
	if err != nil {
		return Target{}, managedssh.TargetQualification{}, ErrRejected
	}
	keyDigest := sha256.Sum256(signer.PublicKey().Marshal())
	for _, profile := range document.Profiles {
		if profile.TargetRef == targetRef && profile.Mode == mode && profile.AuthenticationKey == hex.EncodeToString(keyDigest[:]) {
			target := Target{Address: profile.Address, Port: profile.Port, User: profile.User, HostKeySHA256: profile.HostKeyDigest, HostKeyAlgorithm: profile.HostKeyAlgorithm}
			if target.Validate() != nil {
				return Target{}, managedssh.TargetQualification{}, ErrRejected
			}
			return target, profile, nil
		}
	}
	return Target{}, managedssh.TargetQualification{}, ErrRejected
}

// QualifiedManaged takes the helper profile only from trusted deployment evidence; employee-supplied configuration cannot select another UID, namespace, executable or receipt store.
// QualifiedManaged берёт helper profile только из trusted deployment evidence; employee-supplied configuration не может выбрать другой UID, namespace, executable либо receipt store.
func (client Client) QualifiedManaged(ctx context.Context, operator OperatorQualifications, targetRef string, target Target, privateKey []byte, request ManagedRequest) (managedssh.Reply, error) {
	profile, err := operator.Resolve(targetRef, target, privateKey, managedssh.ManagedTransport)
	if err != nil {
		return managedssh.Reply{}, err
	}
	return client.managed(ctx, target, privateKey, *profile.Profile, request, func() error {
		_, err := operator.Resolve(targetRef, target, privateKey, managedssh.ManagedTransport)
		return err
	})
}

// QualifiedExecute requires a separately operator-qualified non-root execute identity; the owner must already have admitted EACH consent for the exact command/input digest and current target generation.
// QualifiedExecute требует отдельно operator-qualified non-root execute identity; owner обязан заранее подтвердить EACH consent на exact command/input digest и current target generation.
func (client Client) QualifiedExecute(ctx context.Context, operator OperatorQualifications, targetRef string, target Target, privateKey []byte, command Command) (Result, error) {
	profile, err := operator.Resolve(targetRef, target, privateKey, managedssh.ExecuteTransport)
	if err != nil {
		return Result{}, err
	}
	return client.run(ctx, target, privateKey, command, &profile, func() error {
		_, err := operator.Resolve(targetRef, target, privateKey, managedssh.ExecuteTransport)
		return err
	})
}
