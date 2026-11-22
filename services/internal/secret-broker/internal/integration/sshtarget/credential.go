package sshtarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// LoadCredential admits only the dedicated SSH identity material port and exact owner pins; generic TOKEN projection and other broker purposes have no fallback path.
// LoadCredential допускает только отдельный SSH identity material port и exact owner pins; generic TOKEN projection и другие broker purposes не имеют fallback path.
func LoadCredential(ctx context.Context, reader sshrepository.IdentityMaterialReader, locator value.SSHIdentityLocator) ([]byte, error) {
	if ctx == nil || reader == nil || !locator.Valid() {
		return nil, ErrRejected
	}
	if ctx.Err() != nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	material, err := reader.ReadSSHIdentity(ctx, locator)
	if err != nil {
		clear(material)
		var rejection *sshrepository.IdentityAcknowledgementRejection
		switch {
		case errors.Is(err, sshrepository.ErrIdentityUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			return nil, sshrepository.ErrIdentityUnavailable
		case errors.As(err, &rejection):
			return nil, rejection
		case errors.Is(err, sshrepository.ErrIdentityAbsent):
			return nil, sshrepository.ErrIdentityAbsent
		case errors.Is(err, sshrepository.ErrIdentityRejected):
			return nil, ErrRejected
		default:
			return nil, sshrepository.ErrIdentityUnavailable
		}
	}
	if len(material) == 0 || len(material) > 16384 {
		clear(material)
		return nil, ErrRejected
	}
	metadata, err := PrepareCredential(material)
	if ctx.Err() != nil {
		clear(material)
		return nil, sshrepository.ErrIdentityUnavailable
	}
	if err != nil || metadata.ContentSHA256 != locator.ContentSHA256 || metadata.AuthenticationSHA256 != locator.AuthenticationSHA256 {
		clear(material)
		return nil, ErrRejected
	}
	return material, nil
}

// PrepareCredential parses the bounded actual key before the owner authorizes a material write, rejecting unsupported/encrypted/legacy keys without SDK diagnostics and deriving both immutable pins inside broker custody.
// PrepareCredential разбирает bounded actual key до owner authorization material write, отклоняя unsupported/encrypted/legacy keys без SDK diagnostics и выводя оба immutable pins внутри broker custody.
func PrepareCredential(material []byte) (value.SSHIdentityKeyMetadata, error) {
	if len(material) == 0 || len(material) > 16384 {
		return value.SSHIdentityKeyMetadata{}, ErrRejected
	}
	snapshot := bytes.Clone(material)
	defer clear(snapshot)
	signer, err := authenticationSigner(snapshot)
	if err != nil {
		return value.SSHIdentityKeyMetadata{}, ErrRejected
	}
	content, public := sha256.Sum256(snapshot), sha256.Sum256(signer.PublicKey().Marshal())
	return value.SSHIdentityKeyMetadata{ContentSHA256: hex.EncodeToString(content[:]), AuthenticationSHA256: hex.EncodeToString(public[:])}, nil
}

// KeyPreparer supplies the purpose-specific domain port without exposing SSH SDK key objects or parser diagnostics beyond broker custody.
// KeyPreparer предоставляет purpose-specific domain port без выдачи SSH SDK key objects либо parser diagnostics за broker custody.
type KeyPreparer struct{}

// PrepareSSHIdentity delegates to the same actual key parser used by the private material reader, retaining no input and returning only integrity metadata.
// PrepareSSHIdentity использует тот же actual key parser, что private material reader, не сохраняя input и возвращая только integrity metadata.
func (KeyPreparer) PrepareSSHIdentity(material []byte) (value.SSHIdentityKeyMetadata, error) {
	return PrepareCredential(material)
}
