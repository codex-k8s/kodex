package value

import "time"

// ArtifactScan назначается только доверенным серверным сканером, не transport payload.
// Digest/size относятся к точным байтам INSTREAM; object receipt добавляет owner.
type ArtifactScan struct {
	Engine, Policy, Digest string
	SizeBytes              int64
	ScannedAt              time.Time
	Infected               bool
}

const ArtifactAntivirusPolicy = "clamav-instream-v1"
