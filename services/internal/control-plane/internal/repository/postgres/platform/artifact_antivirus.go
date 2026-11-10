package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ConfigureArtifactScanner rejects a nil scanner and replaces the repository scanner used by
// subsequent uploads. This composition setter performs no scan or synchronization and does not
// certify existing content.
// ConfigureArtifactScanner отклоняет nil scanner и заменяет repository scanner для последующих
// uploads. Composition setter не выполняет scan либо synchronization и не подтверждает existing
// content.
func (repository *Repository) ConfigureArtifactScanner(scanner artifactpolicy.Scanner) error {
	if scanner == nil {
		return errs.ErrInvalid
	}
	repository.artifactScanner = scanner
	return nil
}

// ConfigureArtifactAntivirusMode selects a deployment-owned admission policy for ordinary files.
// Empty/required retains AV admission; disabled records NOT_SCANNED, never scanner success. It is
// a startup-only setter; unknown values fail without changing the previous mode.
// ConfigureArtifactAntivirusMode выбирает политику установки для обычных файлов. Пустой/required
// сохраняет AV, disabled записывает NOT_SCANNED, никогда успех scanner. Setter вызывается при
// startup; неизвестное значение отклоняется без изменения прежнего режима.
func (repository *Repository) ConfigureArtifactAntivirusMode(mode string) error {
	switch mode {
	case "", "required":
		repository.artifactAntivirusDisabled = false
	case "disabled":
		repository.artifactAntivirusDisabled = true
	default:
		return errs.ErrInvalid
	}
	return nil
}

// scanArtifact validates reader/size/digest, requires a configured scanner, rewinds before and
// after ScanReader and requires exact reported digest/size plus receipt metadata. Seek/scan errors
// and mismatched content fail closed; an infected valid report is returned for quarantine rather
// than a clean verdict. It runs outside the owner transaction and does not persist bytes or proof.
// scanArtifact проверяет reader/size/digest, требует configured scanner, перематывает до и после
// ScanReader и требует exact reported digest/size с receipt metadata. Seek/scan errors и
// mismatched content закрыто отклоняются; valid infected report возвращается для quarantine вместо
// CLEAN. Работает вне owner transaction, не сохраняет bytes либо proof.
func (repository *Repository) scanArtifact(ctx context.Context, reader io.ReadSeeker, digest string, size int64) (value.ArtifactScan, error) {
	if reader == nil || size < 0 || size > maximumArtifactBytes || !objectDigestValid(digest) {
		return value.ArtifactScan{}, errs.ErrInvalid
	}
	if repository.artifactAntivirusDisabled {
		return inspectUnscannedArtifact(ctx, reader, digest, size)
	}
	return repository.scanRequiredArtifact(ctx, reader, digest, size)
}

// scanRequiredArtifact always uses the real configured scanner, regardless of the ordinary-file
// mode. Executable repository bundles use this path; unavailable never falls back to format-only.
// scanRequiredArtifact всегда использует настроенный scanner независимо от режима обычных файлов.
// Исполняемые repository bundles используют этот путь; unavailable не включает format-only.
func (repository *Repository) scanRequiredArtifact(ctx context.Context, reader io.ReadSeeker, digest string, size int64) (value.ArtifactScan, error) {
	if reader == nil || size < 0 || size > maximumArtifactBytes || !objectDigestValid(digest) {
		return value.ArtifactScan{}, errs.ErrInvalid
	}
	if repository.artifactScanner == nil {
		return value.ArtifactScan{}, errs.ErrUnavailable
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return value.ArtifactScan{}, errs.ErrInvalid
	}
	report, err := repository.artifactScanner.ScanReader(ctx, reader, size)
	if _, seekErr := reader.Seek(0, io.SeekStart); seekErr != nil {
		return value.ArtifactScan{}, errs.ErrUnavailable
	}
	if err != nil {
		return value.ArtifactScan{}, errs.ErrUnavailable
	}
	if report.Digest != digest || report.SizeBytes != size || report.NotScanned || !validArtifactAntivirus(report) {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	return report, nil
}

// inspectUnscannedArtifact hashes a bounded seekable body, checks exact size/digest and cancellation,
// and rewinds it. Only explicit disabled composition reaches this path; format admission still
// belongs to the producer and this receipt explicitly carries no antivirus attestation.
// inspectUnscannedArtifact хеширует ограниченное seekable тело, проверяет точные размер/хеш и отмену,
// затем перематывает. Путь доступен только при явном disabled; проверка формата остаётся у producer,
// receipt явно не содержит антивирусной аттестации.
func inspectUnscannedArtifact(ctx context.Context, reader io.ReadSeeker, digest string, size int64) (value.ArtifactScan, error) {
	if ctx.Err() != nil {
		return value.ArtifactScan{}, errs.ErrUnavailable
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return value.ArtifactScan{}, errs.ErrInvalid
	}
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(reader, size+1))
	_, seekErr := reader.Seek(0, io.SeekStart)
	if err != nil || seekErr != nil || ctx.Err() != nil {
		return value.ArtifactScan{}, errs.ErrUnavailable
	}
	if read != size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	return value.ArtifactScan{Engine: "format-policy-v1", Policy: value.ArtifactFormatOnlyPolicy,
		Digest: digest, SizeBytes: size, CheckedAt: time.Now().UTC(), NotScanned: true}, nil
}

// artifactAdmissionState preserves format quarantine/failure and assigns NOT_SCANNED only to a
// format-admitted ordinary file with an explicit server-issued no-AV receipt.
// artifactAdmissionState сохраняет форматный карантин/отказ и присваивает NOT_SCANNED только
// допустимому по формату обычному файлу с явным серверным receipt об отсутствии AV.
func artifactAdmissionState(format string, report value.ArtifactScan) string {
	if format == artifactpolicy.ScanClean && report.NotScanned {
		return artifactpolicy.ScanNotScanned
	}
	return format
}

// validArtifactAntivirus validates one of two disjoint server receipt shapes. A real ClamAV scan
// has ScannedAt; an explicit format-only report has CheckedAt and no infection/scan attestation.
// Both reject future timestamps beyond clock tolerance. Digest/size require the exact ledger.
// validArtifactAntivirus проверяет две непересекающиеся формы серверного receipt: реальный ClamAV
// со ScannedAt либо явный format-only с CheckedAt без аттестации AV. Будущее сверх допуска часов
// отклоняется в обоих режимах. Хеш/размер отдельно связываются точным ledger.
func validArtifactAntivirus(report value.ArtifactScan) bool {
	if report.NotScanned {
		return report.Policy == value.ArtifactFormatOnlyPolicy && report.Engine == "format-policy-v1" &&
			!report.Infected && report.ScannedAt.IsZero() && !report.CheckedAt.IsZero() && time.Since(report.CheckedAt) >= -time.Minute
	}
	return report.Policy == value.ArtifactAntivirusPolicy && strings.HasPrefix(report.Engine, "ClamAV ") &&
		len(report.Engine) <= 256 && !strings.ContainsAny(report.Engine, "\x00\r\n") &&
		!report.ScannedAt.IsZero() && report.CheckedAt.IsZero() && time.Since(report.ScannedAt) >= -time.Minute
}

// artifactAntivirusReceipt stores engine/policy/time/verdict only; exact digest/size/object pins
// remain in the surrounding immutable ledger row. This JSON is not a self-authenticating receipt
// or a substitute for those content bindings.
// artifactAntivirusReceipt хранит только engine/policy/time/verdict; exact digest/size/object pins
// остаются в surrounding immutable ledger row. JSON не является self-authenticating receipt и не
// заменяет content bindings.
type artifactAntivirusReceipt struct {
	Engine    string     `json:"engine"`
	Policy    string     `json:"policy"`
	ScannedAt *time.Time `json:"scannedAt,omitempty"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	Verdict   string     `json:"verdict"`
}

// artifactAntivirusJSON validates report metadata and explicitly encodes NOT_SCANNED or a real AV verdict
// while omitting external ledger content pins. It neither signs the receipt nor rescans content or
// proves the report came from a live scanner.
// artifactAntivirusJSON проверяет metadata и явно кодирует NOT_SCANNED либо реальный AV verdict,
// не дублируя external ledger content pins. Не подписывает receipt, не сканирует content повторно
// и не доказывает live scanner origin.
func artifactAntivirusJSON(report value.ArtifactScan) ([]byte, error) {
	if !validArtifactAntivirus(report) {
		return nil, errs.ErrConflict
	}
	verdict := "CLEAN"
	if report.Infected {
		verdict = "QUARANTINED"
	}
	receipt := artifactAntivirusReceipt{Engine: report.Engine, Policy: report.Policy, Verdict: verdict}
	if report.NotScanned {
		receipt.Verdict, receipt.CheckedAt = "NOT_SCANNED", &report.CheckedAt
	} else {
		receipt.ScannedAt = &report.ScannedAt
	}
	return json.Marshal(receipt)
}

// decodeArtifactAntivirus decodes the existing ordinary JSON receipt, accepts only the two
// AV verdicts or explicit NOT_SCANNED and rebuilds a report with caller-supplied content pins.
// Invalid input is Conflict; unknown JSON fields are not rejected here and supplied content pins
// still require an authoritative ledger.
// decodeArtifactAntivirus декодирует JSON receipt, различает реальный AV и явный NOT_SCANNED
// и восстанавливает report с caller-supplied digest/size до metadata validation. Invalid input
// даёт Conflict; unknown JSON fields здесь не отклоняются, supplied content pins требуют
// authoritative ledger.
func decodeArtifactAntivirus(raw []byte, digest string, size int64) (value.ArtifactScan, error) {
	var receipt artifactAntivirusReceipt
	if json.Unmarshal(raw, &receipt) != nil || (receipt.Verdict != "CLEAN" && receipt.Verdict != "QUARANTINED" && receipt.Verdict != "NOT_SCANNED") {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	report := value.ArtifactScan{Engine: receipt.Engine, Policy: receipt.Policy,
		Digest: digest, SizeBytes: size, Infected: receipt.Verdict == "QUARANTINED", NotScanned: receipt.Verdict == "NOT_SCANNED"}
	if receipt.ScannedAt != nil {
		report.ScannedAt = *receipt.ScannedAt
	}
	if receipt.CheckedAt != nil {
		report.CheckedAt = *receipt.CheckedAt
	}
	if !validArtifactAntivirus(report) {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	return report, nil
}
