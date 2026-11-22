package managedssh

import "time"

// HealthAdmission is the private finite owner-to-broker protocol; locators establish no authority independently of the original live claim.
// HealthAdmission является private finite owner-to-broker protocol; locators не устанавливают authority отдельно от original live claim.
type HealthAdmission struct {
	AttemptRef, SnapshotDigest string
	Identity                   IdentityLocator
	Mode                       TransportMode
	ExpiresAt                  time.Time
}

// HealthObservation contains a joined fixed-read authentication fact without command output, key material or resource permission.
// HealthObservation содержит joined fixed-read authentication fact без command output, key material либо resource permission.
type HealthObservation struct {
	ObservedSubjectDigest string
	QualifiedUntil        time.Time
}

// HealthReceipt acknowledges the original owner test only; a discarded fact cannot become credential or resource authority.
// HealthReceipt подтверждает лишь original owner test; discarded fact не становится credential либо resource authority.
type HealthReceipt struct {
	TestRef, State string
	Applied        bool
}
