package artifactpolicy

import (
	"context"
	"io"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Scanner accepts only a caller-provided reader and size for context-bound artifact scanning, with
// no filesystem path or authority to read another file.
// Scanner принимает только переданные caller reader и размер для проверки artifact с context, без
// filesystem пути либо полномочий чтения другого файла.
type Scanner interface {
	// ScanReader returns the scanner result for the supplied bounded reader under caller context;
	// implementation owns scan policy rather than discovering files by path.
	// ScanReader возвращает результат scanner для переданного ограниченного reader под caller context;
	// реализация определяет scan policy вместо поиска файлов по пути.
	ScanReader(context.Context, io.Reader, int64) (value.ArtifactScan, error)
}
