package entity

import "time"

type ArtifactBindingTarget struct {
	AgentRef, Name, State, BindReason, UnbindReason string
	AgentVersion                                    int64
	Bound, CanBind, CanUnbind                       bool
}

type ArtifactBindingTargets struct {
	ArtifactRef, ProjectRef, Digest, NextPageToken string
	ArtifactVersion, Total                         int64
	Items                                          []ArtifactBindingTarget
	EvaluatedAt                                    time.Time
}

type RunAttachmentEligibility struct {
	ProjectRef, RunRef, WorkflowVersionRef, Reason, Digest string
	Target                                                 RunTarget
	RunVersion                                             int64
	Eligible                                               bool
	EvaluatedAt                                            time.Time
}
