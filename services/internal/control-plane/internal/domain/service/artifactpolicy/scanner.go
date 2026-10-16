package artifactpolicy

import (
	"context"
	"io"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Scanner не принимает путь на диске и не получает полномочий читать другие файлы.
type Scanner interface {
	ScanReader(context.Context, io.Reader, int64) (value.ArtifactScan, error)
}
