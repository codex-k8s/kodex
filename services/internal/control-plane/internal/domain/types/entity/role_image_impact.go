package entity

import "time"

type RoleImageImpactPlan struct {
	Ref, ConfigurationRef, RevisionRef, RevisionDigest                      string
	RecipeRef, BuildRef, ArtifactRef, ArtifactDigest, AdmissionPolicyDigest string
	Digest, State                                                           string
	Version, ConfigurationVersion, RecipeGeneration, Total                  int64
	CreatedAt, ExpiresAt                                                    time.Time
}

type RoleImageImpactItem struct {
	Ref, EnvironmentRef, SourceVersionRef, SourceVersionDigest string
	EnvironmentVersion                                         int64
	Consumer                                                   RuntimeEnvironmentConsumer
	Outcome, ResultEnvironmentVersionRef, ResultBindingRef     string
	ResultBindingVersion                                       int64
}

type RoleImageImpactPage struct {
	Plan          RoleImageImpactPlan
	Items         []RoleImageImpactItem
	Total         int64
	NextPageToken string
}
