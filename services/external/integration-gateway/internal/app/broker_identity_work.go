package app

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/protobuf/proto"
)

// brokerIdentityWorkUnavailable conceals private owner and transport diagnostics at the common dispatcher.
// brokerIdentityWorkUnavailable скрывает private owner и transport diagnostics в общем dispatcher.
var brokerIdentityWorkUnavailable = errors.New("broker identity work is unavailable")

// healthClaimRepresentation refuses absent, unknown or mixed fresh claim metadata before either consumer branch.
// It validates the exact broker package and READ boundary, but private owner admission remains mandatory.
// healthClaimRepresentation отвергает absent, unknown либо mixed metadata свежего claim до любой consumer branch.
// Проверяет exact broker package и READ boundary; private owner admission остаётся обязательным.
func healthClaimRepresentation(claim *cp.IntegrationConnectionTestClaim) (cp.IntegrationCredentialDeliveryKind, error) {
	if claim == nil || len(claim.ProtoReflect().GetUnknown()) != 0 || !workInputDigest(claim.GetInputDigest()) {
		return 0, brokerIdentityWorkUnavailable
	}
	kind := claim.GetCredentialDeliveryKind()
	broker := claim.GetBrokerIdentityPurpose() != 0 || claim.GetBrokerIdentityRef() != "" || claim.GetBrokerIdentityRevision() != 0
	switch kind {
	case cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_NONE:
		if broker || claim.GetCredentialRevision() != nil || claim.GetCredentialMaterializationRef() != "" {
			return 0, brokerIdentityWorkUnavailable
		}
	case cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN:
		if broker {
			return 0, brokerIdentityWorkUnavailable
		}
	case cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY:
		definition, err := integrationpackage.Parse(claim.GetDefinitionPackage())
		delivery, known := definition.ConnectionCredentialDelivery()
		health, found := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
		purpose := strings.TrimPrefix(claim.GetBrokerIdentityPurpose().String(), "INTEGRATION_BROKER_IDENTITY_PURPOSE_")
		if err != nil || !known || !found || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity ||
			definition.Metadata.Key != claim.GetDefinitionKey() || definition.Metadata.Version != claim.GetDefinitionVersion() || definition.Digest != claim.GetDefinitionDigest() ||
			definition.Spec.AdapterOwner != "integration-gateway" || definition.Spec.ExecutionRoute != "MANAGED_MCP" || health.Risk != "READ" || health.ApprovalPolicy != "NONE" ||
			(purpose != "KUBERNETES_TARGET" && purpose != "SSH_TARGET") || delivery.BrokerPurpose != purpose ||
			!brokerWorkReference(claim.GetBrokerIdentityRef()) || claim.GetBrokerIdentityRevision() < 1 || claim.GetBrokerIdentityRevision() > 9007199254740991 ||
			claim.GetCredentialRevision() != nil || claim.GetCredentialMaterializationRef() != "" {
			return 0, brokerIdentityWorkUnavailable
		}
	default:
		return 0, brokerIdentityWorkUnavailable
	}
	return kind, nil
}

// runBrokerIdentityProbe preserves exact lease coordinates and counts attempted work before validating a later owner response.
// It never invokes generic Begin or Complete, retries a probe, or interprets an error as an authentication denial.
// runBrokerIdentityProbe сохраняет exact lease coordinates и учитывает attempted work до проверки позднего owner response.
// Не вызывает generic Begin/Complete, не повторяет probe и не считает error authentication denial.
func runBrokerIdentityProbe(ctx context.Context, consumers *brokerIdentityConsumers, claim *cp.IntegrationConnectionTestClaim, personal bool, config Config, record func(bool)) (int, error) {
	lease := claim.GetLease()
	consumer := consumers.forPurpose(claim.GetBrokerIdentityPurpose())
	if ctx == nil || consumer == nil || record == nil || lease == nil || len(lease.ProtoReflect().GetUnknown()) != 0 || lease.GetRef() == "" || lease.GetFence() == "" || lease.GetGeneration() != 1 ||
		lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || !lease.GetExpiresAt().AsTime().After(time.Now().Add(config.RequestTimeout)) || claim.GetTestRef() == "" {
		return 0, brokerIdentityWorkUnavailable
	}
	operation, cancel := context.WithDeadline(ctx, lease.GetExpiresAt().AsTime())
	defer cancel()
	var receipt brokerIdentityProbeReceipt
	var err error
	work := proto.Clone(claim).(*cp.IntegrationConnectionTestClaim)
	if personal {
		receipt, err = consumer.ProbePersonal(operation, work)
	} else {
		receipt, err = consumer.ProbeShared(operation, work)
	}
	if receipt.Attempted {
		record(receipt.Authenticated)
	}
	processed := 0
	if receipt.Attempted || receipt.TestRef != "" {
		processed = 1
	}
	if err != nil {
		return processed, brokerIdentityWorkUnavailable
	}
	if receipt.TestRef != claim.GetTestRef() || (receipt.State != "SUCCEEDED" && receipt.State != "FAILED" && receipt.State != "CANCELLED") ||
		(receipt.State == "CANCELLED" && receipt.Applied) || (receipt.State != "CANCELLED" && !receipt.Applied) ||
		(receipt.State == "SUCCEEDED" && !receipt.Authenticated) || (receipt.State == "FAILED" && receipt.Authenticated) {
		return processed, brokerIdentityWorkUnavailable
	}
	return 1, nil
}

// workInputDigest accepts only canonical lowercase SHA256; owner snapshot bytes remain authoritative.
// workInputDigest принимает лишь canonical lowercase SHA256; owner snapshot bytes остаются authoritative.
func workInputDigest(input string) bool {
	if len(input) != 64 || strings.ToLower(input) != input {
		return false
	}
	_, err := hex.DecodeString(input)
	return err == nil
}

// brokerWorkReference validates bounded opaque metadata syntax without interpreting it as authority.
// brokerWorkReference проверяет bounded opaque metadata syntax, не считая её authority.
func brokerWorkReference(ref string) bool {
	if len(ref) < 8 || len(ref) > 96 {
		return false
	}
	for _, c := range ref {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
