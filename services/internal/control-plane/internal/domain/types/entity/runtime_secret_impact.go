package entity

type RuntimeSecretImpactConsumer struct {
	EnvironmentRef, EnvironmentVersionRef string
	EnvironmentVersion                    int64
	SecretRevisions                       []int64
	Consumer                              RuntimeEnvironmentConsumer
}

type RuntimeSecretImpact struct {
	SecretRef                            string
	SecretVersion, TargetRevision, Total int64
	Consumers                            []RuntimeSecretImpactConsumer
	NextPageToken                        string
}

type RuntimeSecretRebindSelection struct {
	EnvironmentRef, SourceVersionRef string
	ExpectedEnvironmentVersion       int64
	Consumers                        []RuntimeEnvironmentConsumer
}
