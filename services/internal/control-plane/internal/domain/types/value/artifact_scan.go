package value

import "time"

// ArtifactScan binds an owner-issued admission report to exact bytes. Real AV uses ScannedAt;
// explicit ordinary-file opt-out uses CheckedAt and NotScanned, never a clean scanner verdict.
// ArtifactScan связывает серверный отчёт допуска с точными байтами. Реальный AV использует
// ScannedAt; явное исключение обычных файлов — CheckedAt и NotScanned, никогда чистый verdict AV.
type ArtifactScan struct {
	Engine, Policy, Digest string
	SizeBytes              int64
	ScannedAt              time.Time
	CheckedAt              time.Time
	Infected               bool
	NotScanned             bool
}

// ArtifactAntivirusPolicy pins the scanner policy revision carried with an
// artifact verdict so a different scanning route is not silently accepted.
// ArtifactAntivirusPolicy закрепляет ревизию политики в заключении по
// артефакту, не позволяя незаметно принять иной маршрут проверки.
const ArtifactAntivirusPolicy = "clamav-instream-v1"

// ArtifactFormatOnlyPolicy identifies an explicit operator choice to admit ordinary artifacts
// without malware scanning. It never proves a clean antivirus verdict or admits executable bundles.
// ArtifactFormatOnlyPolicy обозначает явный выбор оператора принимать обычные артефакты без
// антивируса. Не доказывает чистый AV verdict и не допускает исполняемые bundles.
const ArtifactFormatOnlyPolicy = "artifact-format-only-v1"
