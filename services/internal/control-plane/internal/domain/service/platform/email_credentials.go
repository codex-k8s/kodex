package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func validEmailCredentialValue(kind string, raw []byte) bool {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return false
	}
	switch kind {
	case "CA_CERTIFICATE":
		count := 0
		for len(bytes.TrimSpace(raw)) != 0 {
			raw = bytes.TrimSpace(raw)
			if !bytes.HasPrefix(raw, []byte("-----BEGIN CERTIFICATE-----")) {
				return false
			}
			block, rest := pem.Decode(raw)
			if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
				return false
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil || !certificate.IsCA {
				return false
			}
			count++
			if count > 32 {
				return false
			}
			raw = rest
		}
		return count > 0
	case "USERNAME":
		return len(raw) <= 320 && utf8.Valid(raw) && !strings.ContainsAny(string(raw), "\x00\r\n")
	case "AUTH_SECRET":
		return len(raw) <= 16<<10 && utf8.Valid(raw) && !strings.ContainsAny(string(raw), "\x00\r\n")
	default:
		return false
	}
}

func (service *Service) ConfigureEmailMailboxCredential(ctx context.Context, principal value.Principal, mutation value.Mutation, connectionRef, kind string, raw []byte) (entity.EmailMailboxCredential, error) {
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || *mutation.ExpectedVersion == math.MaxInt64 ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 || connectionRef == "" || !validEmailCredentialValue(kind, raw) {
		return entity.EmailMailboxCredential{}, errs.ErrInvalid
	}
	if service.emailCredentialMaterializer == nil {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	connection, err := service.repository.GetIntegrationConnection(ctx, principal, connectionRef)
	if err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	if connection.DefinitionKey != "email" || !containsString(connection.NextActions, "CONFIGURE_CREDENTIAL") {
		return entity.EmailMailboxCredential{}, errs.ErrForbidden
	}
	// Exact retry может иметь прежнюю OCC version; receipt проверяется владельцем.
	if connection.Version < *mutation.ExpectedVersion {
		return entity.EmailMailboxCredential{}, errs.ErrVersionMismatch
	}
	identity := sha256.Sum256([]byte(strings.Join([]string{principal.AuthorityTenant, principal.ActorID, connectionRef, mutation.IdempotencyKey}, "\x00")))
	name := "email-" + hex.EncodeToString(identity[:16])
	generation := *mutation.ExpectedVersion + 1
	key := name + "." + strconv.FormatInt(generation, 10)
	digest := sha256.Sum256(raw)
	payload := command.EmailCredentialInput{ConnectionRef: connectionRef, Credential: entity.EmailMailboxCredential{
		Name: name, Generation: generation, Kind: kind, ConnectionRef: connectionRef, ContentSHA256: hex.EncodeToString(digest[:]),
	}}
	if connection.Version != *mutation.ExpectedVersion {
		payload.ReplayOnly = true
		return service.commitEmailCredential(ctx, principal, mutation, payload)
	}
	secret := append([]byte(nil), raw...)
	defer clear(secret)
	materialized, err := service.emailCredentialMaterializer.Materialize(ctx, key, secret)
	if errors.Is(err, ErrCredentialMaterializationConflict) {
		return entity.EmailMailboxCredential{}, errs.ErrIdempotencyReuse
	}
	if err != nil {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	if materialized.ContentSHA256 != hex.EncodeToString(digest[:]) || materialized.SecretUID == "" || materialized.SecretResourceVersion == "" ||
		!strings.HasSuffix(materialized.SecretRef, "/email-bridge-mailbox-projection#"+key) {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	payload.Credential.SecretRef, payload.Credential.SecretUID, payload.Credential.SecretResourceVersion = materialized.SecretRef, materialized.SecretUID, materialized.SecretResourceVersion
	return service.commitEmailCredential(ctx, principal, mutation, payload)
}

func (service *Service) commitEmailCredential(ctx context.Context, principal value.Principal, mutation value.Mutation, payload command.EmailCredentialInput) (entity.EmailMailboxCredential, error) {
	result, err := service.executeResolved(ctx, command.Command{Kind: command.ConfigureEmailCredential, Principal: principal, Mutation: mutation, Payload: payload})
	if err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	if result.EmailCredential == nil {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	credential := result.EmailCredential
	if credential.Name != payload.Credential.Name || credential.Generation != payload.Credential.Generation || credential.Kind != payload.Credential.Kind ||
		credential.ConnectionRef != payload.ConnectionRef || credential.ConnectionVersion != credential.Generation || credential.ContentSHA256 != payload.Credential.ContentSHA256 {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	return *result.EmailCredential, nil
}
