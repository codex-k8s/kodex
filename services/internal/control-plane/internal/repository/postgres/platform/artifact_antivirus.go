package platform

import (
	"context"
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
	if report.Digest != digest || report.SizeBytes != size || !validArtifactAntivirus(report) {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	return report, nil
}

// validArtifactAntivirus requires the fixed policy, bounded clean ClamAV engine string and nonzero
// scan time no more than one minute in the future. It imposes no maximum age and does not validate
// digest/size or derive infection status.
// validArtifactAntivirus требует fixed policy, bounded clean ClamAV engine string и nonzero scan
// time не более одной минуты в будущем. Не задаёт maximum age и не проверяет digest/size либо
// infection status.
func validArtifactAntivirus(report value.ArtifactScan) bool {
	return report.Policy == value.ArtifactAntivirusPolicy && strings.HasPrefix(report.Engine, "ClamAV ") &&
		len(report.Engine) <= 256 && !strings.ContainsAny(report.Engine, "\x00\r\n") &&
		!report.ScannedAt.IsZero() && time.Since(report.ScannedAt) >= -time.Minute
}

// artifactAntivirusReceipt stores engine/policy/time/verdict only; exact digest/size/object pins
// remain in the surrounding immutable ledger row. This JSON is not a self-authenticating receipt
// or a substitute for those content bindings.
// artifactAntivirusReceipt хранит только engine/policy/time/verdict; exact digest/size/object pins
// остаются в surrounding immutable ledger row. JSON не является self-authenticating receipt и не
// заменяет content bindings.
type artifactAntivirusReceipt struct {
	Engine    string    `json:"engine"`
	Policy    string    `json:"policy"`
	ScannedAt time.Time `json:"scannedAt"`
	Verdict   string    `json:"verdict"`
}

// artifactAntivirusJSON validates report metadata and encodes CLEAN or QUARANTINED from Infected
// while omitting external ledger content pins. It neither signs the receipt nor rescans content or
// proves the report came from a live scanner.
// artifactAntivirusJSON проверяет report metadata и кодирует CLEAN либо QUARANTINED по Infected,
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
	return json.Marshal(artifactAntivirusReceipt{report.Engine, report.Policy, report.ScannedAt, verdict})
}

// decodeArtifactAntivirus decodes the existing ordinary JSON receipt, accepts only the two
// verdicts and rebuilds a report with caller-supplied digest/size before metadata validation.
// Invalid input is Conflict; unknown JSON fields are not rejected here and supplied content pins
// still require an authoritative ledger.
// decodeArtifactAntivirus декодирует existing ordinary JSON receipt, допускает только два verdicts
// и восстанавливает report с caller-supplied digest/size до metadata validation. Invalid input
// даёт Conflict; unknown JSON fields здесь не отклоняются, supplied content pins требуют
// authoritative ledger.
func decodeArtifactAntivirus(raw []byte, digest string, size int64) (value.ArtifactScan, error) {
	var receipt artifactAntivirusReceipt
	if json.Unmarshal(raw, &receipt) != nil || (receipt.Verdict != "CLEAN" && receipt.Verdict != "QUARANTINED") {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	report := value.ArtifactScan{Engine: receipt.Engine, Policy: receipt.Policy, ScannedAt: receipt.ScannedAt,
		Digest: digest, SizeBytes: size, Infected: receipt.Verdict == "QUARANTINED"}
	if !validArtifactAntivirus(report) {
		return value.ArtifactScan{}, errs.ErrConflict
	}
	return report, nil
}
