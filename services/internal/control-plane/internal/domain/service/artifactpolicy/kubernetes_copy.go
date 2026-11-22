package artifactpolicy

// InspectKubernetesNormalizedCopyReader reuses the existing non-previewable copy representation policy.
// Its caller must first verify exact artifact provenance and NormalizeCopyUpload; this function adds
// no artifact authority, archive normalization or AV claim and never interprets caller media types.
// InspectKubernetesNormalizedCopyReader использует existing copy representation policy без preview.
// Caller сначала проверяет exact artifact provenance и NormalizeCopyUpload; функция не выдаёт
// artifact authority, archive normalization либо AV claim и не трактует caller media types.
func InspectKubernetesNormalizedCopyReader(reader Reader, size int64) (Verdict, error) {
	return InspectKubernetesCapturedReader(KubernetesCaptureCopyTar, "source.tar", "application/x-tar", reader, size)
}
