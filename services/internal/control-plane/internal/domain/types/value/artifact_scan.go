package value

import "time"

// ArtifactScan records the scanner's verdict for exact INSTREAM bytes; only a
// trusted server scanner may assign it, while the object receipt supplies owner.
// ArtifactScan хранит заключение сканера по точным байтам INSTREAM; назначает
// его лишь доверенный серверный сканер, а владельца задаёт квитанция объекта.
type ArtifactScan struct {
	Engine, Policy, Digest string
	SizeBytes              int64
	ScannedAt              time.Time
	Infected               bool
}

// ArtifactAntivirusPolicy pins the scanner policy revision carried with an
// artifact verdict so a different scanning route is not silently accepted.
// ArtifactAntivirusPolicy закрепляет ревизию политики в заключении по
// артефакту, не позволяя незаметно принять иной маршрут проверки.
const ArtifactAntivirusPolicy = "clamav-instream-v1"
