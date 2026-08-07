package value

import (
	"errors"
	"strings"
	"time"
)

// GitReconciliationReceipt — проверенный signed proof exact producer #236.
type GitReconciliationReceipt struct {
	ContractVersion     uint32    `json:"contract_version"`
	Issuer              string    `json:"iss"`
	Purpose             string    `json:"purpose"`
	WorkloadID          string    `json:"workload_id"`
	CallerSPIFFEID      string    `json:"caller_spiffe_id"`
	FullMethod          string    `json:"full_method"`
	ActorID             string    `json:"actor_id"`
	OrganizationID      string    `json:"organization_id"`
	ProjectID           string    `json:"project_id"`
	TargetKind          string    `json:"target_kind"`
	TargetResourceID    string    `json:"target_resource_id,omitempty"`
	TargetStableKey     string    `json:"target_stable_key"`
	SourceRef           string    `json:"source_ref"`
	SourceRevision      uint64    `json:"source_revision"`
	SourceSHA256        string    `json:"source_sha256"`
	CommandIntentSHA256 string    `json:"command_intent_sha256"`
	ReceiptID           string    `json:"jti"`
	ReceiptRevision     uint64    `json:"revision"`
	IssuedAt            time.Time `json:"issued_at"`
	NotBefore           time.Time `json:"not_before"`
	ExpiresAt           time.Time `json:"expires_at"`
}

func (receipt GitReconciliationReceipt) Validate(now time.Time) error {
	if receipt.ContractVersion != 1 || receipt.Issuer == "" || receipt.Purpose != "GIT_RECONCILIATION_RECEIPT" ||
		ValidateStableKey(receipt.WorkloadID) != nil || !strings.HasPrefix(receipt.CallerSPIFFEID, "spiffe://") ||
		!strings.HasPrefix(receipt.FullMethod, "/controlplane.v1.ControlPlaneService/ReconcileGit") ||
		ValidateID(receipt.ActorID) != nil || ValidateID(receipt.OrganizationID) != nil || ValidateID(receipt.ProjectID) != nil ||
		ValidateStableKey(receipt.TargetKind) != nil || (receipt.TargetResourceID != "" && ValidateID(receipt.TargetResourceID) != nil) ||
		ValidateStableKey(receipt.TargetStableKey) != nil || len(receipt.SourceRef) < 8 || len(receipt.SourceRef) > 2048 ||
		strings.ContainsAny(receipt.SourceRef, "\x00\r\n") || receipt.SourceRevision == 0 ||
		!validDigestValue(receipt.SourceSHA256) || !validDigestValue(receipt.CommandIntentSHA256) ||
		ValidateID(receipt.ReceiptID) != nil || receipt.ReceiptRevision == 0 || receipt.IssuedAt.IsZero() ||
		receipt.NotBefore.IsZero() || receipt.ExpiresAt.IsZero() || receipt.IssuedAt.After(now.Add(5*time.Second)) ||
		now.Before(receipt.NotBefore.Add(-5*time.Second)) || !now.Before(receipt.ExpiresAt.Add(5*time.Second)) ||
		!receipt.ExpiresAt.After(receipt.NotBefore) || receipt.ExpiresAt.Sub(receipt.IssuedAt) > 5*time.Minute {
		return errors.New("git reconciliation receipt is invalid")
	}
	return nil
}
