package entity

import "time"

type RuntimeSecretDraftImpactPlan struct {
	Ref, DraftRef, SecretRef, Digest, State            string
	DraftVersion, SecretVersion, SourceRevision, Total int64
	ExpiresAt                                          time.Time
}
type RuntimeSecretDraftImpactItem struct {
	Ref, Outcome, ResultEnvironmentVersionRef, ResultBindingRef string
	ResultBindingVersion                                        int64
	Consumer                                                    RuntimeSecretImpactConsumer
}
type RuntimeSecretDraftImpactPage struct {
	Plan          RuntimeSecretDraftImpactPlan
	Items         []RuntimeSecretDraftImpactItem
	Total         int64
	NextPageToken string
}
