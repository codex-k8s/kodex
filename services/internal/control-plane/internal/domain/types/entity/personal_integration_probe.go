package entity

import "time"

// PersonalIntegrationCredentialProbeAdmissionResult bounds new projected-key reads and provider dispatch after one committed admission without shortening the durable completion lease.
// PersonalIntegrationCredentialProbeAdmissionResult ограничивает новые projected-key reads и provider dispatch после committed admission, не сокращая durable completion lease.
type PersonalIntegrationCredentialProbeAdmissionResult struct {
	AuthorizedUntil time.Time `json:"-"`
}
