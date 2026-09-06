package entity

import "time"

type RevisionImpactPlan struct {
	Ref, Kind, SourceRef, SourceRevisionRef, DraftRef string
	Version, SourceVersion, DraftVersion              int64
	TargetDigest, Digest, State, PublishedRevisionRef string
	Total                                             int64
	CreatedAt, ExpiresAt                              time.Time
}

type RevisionImpactItem struct {
	Ref, ProjectRef, ConsumerKind, ConsumerRef  string
	ConsumerVersion                             int64
	BindingRef                                  string
	BindingVersion                              int64
	SourceRevisionRef, Outcome                  string
	ResultRevisionRef, ResultBindingRef         string
	ResultBindingVersion, ResultConsumerVersion int64
}

type RevisionImpactPage struct {
	Plan          RevisionImpactPlan
	Items         []RevisionImpactItem
	Total         int64
	NextPageToken string
}
