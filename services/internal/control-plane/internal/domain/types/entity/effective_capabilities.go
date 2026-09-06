package entity

import "time"

type EffectiveCapability struct {
	Key, Name, Description, Source, Reason    string
	Requested, Required, Effective, Grantable bool
	ConnectionRef, GrantRef, DefinitionDigest string
	ConnectionVersion, GrantVersion           int64
}

type AgentEffectiveCapabilities struct {
	AgentRef, ProjectRef, RuntimeConfigurationRef, EnvironmentVersionRef string
	WorkflowRef, WorkflowVersionRef, StepKey, Digest                     string
	AgentVersion, RuntimeConfigurationVersion                            int64
	RuntimeReady                                                         bool
	EvaluatedAt                                                          time.Time
	Items                                                                []EffectiveCapability
	Total                                                                int64
	NextPageToken                                                        string
}
