package entity

type InteractionIdentity struct {
	Ref, ConnectionRef, ExternalTeamRef, ExternalChannelRef, ExternalUserDigest, SubjectRef, State string
	Version, ConnectionVersion                                                                     int64
}
