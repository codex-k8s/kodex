package artifactpolicy

import "io"

// KubernetesCapturedContentKind is selected only after the owner validated immutable stream
// provenance and any copy archive normalization. It is not an upload or request media-type flag.
// KubernetesCapturedContentKind назначается только после owner validation immutable stream
// provenance и copy archive normalization. Это не upload либо request media-type flag.
type KubernetesCapturedContentKind string

const (
	// KubernetesCaptureText preserves the ordinary text format and preview checks.
	// KubernetesCaptureText сохраняет ordinary text format и preview checks.
	KubernetesCaptureText KubernetesCapturedContentKind = "STREAM_TEXT"
	// KubernetesCaptureOpaque admits captured transport bytes with no inline preview or execution.
	// KubernetesCaptureOpaque допускает captured transport bytes без inline preview либо execution.
	KubernetesCaptureOpaque KubernetesCapturedContentKind = "STREAM_OPAQUE"
	// KubernetesCaptureCopyFile identifies the one regular file extracted by the closed copy recipe.
	// KubernetesCaptureCopyFile определяет один regular file, извлечённый closed copy recipe.
	KubernetesCaptureCopyFile KubernetesCapturedContentKind = "COPY_FILE"
	// KubernetesCaptureCopyTar identifies an already normalized safe-member archive, never a host path.
	// KubernetesCaptureCopyTar определяет normalized safe-member archive, не host path.
	KubernetesCaptureCopyTar KubernetesCapturedContentKind = "COPY_TAR"
)

// InspectKubernetesCapturedReader preserves ordinary quarantine for active/executable formats,
// while admitting owner-validated opaque capture as a non-previewable file. CLEAN here is only
// format compatibility: the caller must still store the exact canonical AV/NOT_SCANNED evidence.
// It performs no source authorization, archive extraction or code execution.
// InspectKubernetesCapturedReader сохраняет ordinary quarantine active/executable formats,
// допуская owner-validated opaque capture только как file без preview. CLEAN здесь означает лишь
// format compatibility: caller сохраняет exact canonical AV/NOT_SCANNED evidence.
// Не авторизует source, не извлекает archive и не исполняет code.
func InspectKubernetesCapturedReader(kind KubernetesCapturedContentKind, fileName, declared string, reader Reader, size int64) (Verdict, error) {
	switch kind {
	case KubernetesCaptureText:
		return InspectReader(fileName, declared, reader, size)
	case KubernetesCaptureOpaque, KubernetesCaptureCopyFile:
		if declared != "application/octet-stream" {
			return Verdict{MediaType: "application/octet-stream", ScanState: ScanFailed, PreviewState: PreviewUnavailable}, nil
		}
	case KubernetesCaptureCopyTar:
		if declared != "application/x-tar" {
			return Verdict{MediaType: "application/octet-stream", ScanState: ScanFailed, PreviewState: PreviewUnavailable}, nil
		}
	default:
		return Verdict{MediaType: "application/octet-stream", ScanState: ScanFailed, PreviewState: PreviewUnavailable}, nil
	}
	verdict, err := InspectReader(fileName, declared, reader, size)
	if err != nil || verdict.ScanState == ScanQuarantined {
		return verdict, err
	}
	if reader == nil || size < 0 {
		return Verdict{}, io.ErrUnexpectedEOF
	}
	return Verdict{MediaType: declared, ScanState: ScanClean, PreviewState: PreviewUnavailable}, nil
}
