package platform

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"time"
)

// kubernetesCaptureSource pins historical provenance and an ACK cutoff, never a live execution
// grant. Stages are read in bounded pages at or below the watermark and acknowledged before seal.
// kubernetesCaptureSource закрепляет historical provenance и ACK cutoff, не live execution grant.
// Stages читаются bounded pages до watermark с ACK, подтверждённым до seal.
type kubernetesCaptureSource struct {
	Version               uint32                             `json:"version"`
	OrganizationID        string                             `json:"organizationId"`
	OrganizationRef       string                             `json:"organizationRef"`
	RootActorID           string                             `json:"rootActorId"`
	RootActorRef          string                             `json:"rootActorRef"`
	ProjectID             string                             `json:"projectId"`
	ProjectRef            string                             `json:"projectRef"`
	RunID                 string                             `json:"runId"`
	RunRef                string                             `json:"runRef"`
	RootRunID             string                             `json:"rootRunId"`
	RootRunRef            string                             `json:"rootRunRef"`
	NodeID                string                             `json:"nodeId"`
	NodeRef               string                             `json:"nodeRef"`
	InvocationRef         string                             `json:"invocationRef"`
	StreamRef             string                             `json:"streamRef"`
	Generation            int64                              `json:"generation"`
	Operation             string                             `json:"operation"`
	CredentialPolicy      string                             `json:"credentialPolicy"`
	SubjectID             string                             `json:"subjectId"`
	SubjectRef            string                             `json:"subjectRef"`
	SubjectKind           string                             `json:"subjectKind"`
	IdentityRef           string                             `json:"identityRef"`
	IdentityRevision      int64                              `json:"identityRevision"`
	BindingDigest         string                             `json:"bindingDigest"`
	SnapshotDigest        string                             `json:"snapshotDigest"`
	ChildInputDigest      string                             `json:"childInputDigest"`
	PlanRef               string                             `json:"planRef"`
	PlanDigest            string                             `json:"planDigest"`
	TerminalDigest        string                             `json:"terminalDigest"`
	TargetRef             string                             `json:"targetRef"`
	TargetRevision        int64                              `json:"targetRevision"`
	ConnectionRef         string                             `json:"connectionRef"`
	Resource              managedkubernetes.ResourceAddress  `json:"resource"`
	OriginalLeaseRef      string                             `json:"originalLeaseRef"`
	OriginalFenceDigest   string                             `json:"originalFenceDigest"`
	OutputWatermark       uint64                             `json:"outputWatermark"`
	MaximumTotalBytes     uint64                             `json:"maximumTotalBytes"`
	SealedAt              time.Time                          `json:"sealedAt"`
	CaptureGeneration     int64                              `json:"captureGeneration"`
	PreviousCaptureRef    string                             `json:"previousCaptureRef"`
	AcknowledgedWatermark uint64                             `json:"acknowledgedWatermark"`
	OwnerState            string                             `json:"ownerState"`
	NativeTerminalKnown   bool                               `json:"nativeTerminalKnown"`
	TransportJoined       bool                               `json:"transportJoined"`
	EffectRef             string                             `json:"effectRef"`
	EffectGeneration      int64                              `json:"effectGeneration"`
	TerminalReceipt       *managedkubernetes.TerminalReceipt `json:"terminalReceipt"`
	LossBytes             uint64                             `json:"lossBytes"`
	LossFacts             uint64                             `json:"lossFacts"`
}
