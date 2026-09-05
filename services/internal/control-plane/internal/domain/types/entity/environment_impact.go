package entity

type RuntimeEnvironmentConsumer struct {
	AgentRef, BindingRef, VersionRef, ProjectRef string
	AgentVersion, BindingVersion                 int64
}

type RuntimeEnvironmentImpact struct {
	EnvironmentRef, TargetVersionRef, TargetDigest, NextPageToken string
	EnvironmentVersion, Total                                     int64
	Consumers                                                     []RuntimeEnvironmentConsumer
}
