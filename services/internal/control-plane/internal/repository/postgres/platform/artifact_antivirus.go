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

func (repository *Repository) ConfigureArtifactScanner(scanner artifactpolicy.Scanner) error {
	if scanner == nil {
		return errs.ErrInvalid
	}
	repository.artifactScanner = scanner
	return nil
}

// Проверка выполняется вне owner transaction после исходного preflight.
// Непроверенный либо несовпадающий поток никогда не получает CLEAN.
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

func validArtifactAntivirus(report value.ArtifactScan) bool {
	return report.Policy == value.ArtifactAntivirusPolicy && strings.HasPrefix(report.Engine, "ClamAV ") &&
		len(report.Engine) <= 256 && !strings.ContainsAny(report.Engine, "\x00\r\n") &&
		!report.ScannedAt.IsZero() && time.Since(report.ScannedAt) >= -time.Minute
}

type artifactAntivirusReceipt struct {
	Engine    string    `json:"engine"`
	Policy    string    `json:"policy"`
	ScannedAt time.Time `json:"scannedAt"`
	Verdict   string    `json:"verdict"`
}

// Digest/size/object pins находятся в той же immutable строке receipt, поэтому
// не дублируются в JSON и не переживают очистку исходных полей ledger.
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
