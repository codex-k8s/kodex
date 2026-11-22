package artifactpolicy

import (
	"bytes"
	"testing"
)

// TestKubernetesCaptureInspectionKeepsOpaqueClosed checks that only the explicit captured-byte
// recipe admits binary data without preview; ordinary inspection and active-content quarantine stay.
// TestKubernetesCaptureInspectionKeepsOpaqueClosed проверяет, что binary data без preview допускает
// только explicit captured-byte recipe; ordinary inspection и active-content quarantine сохраняются.
func TestKubernetesCaptureInspectionKeepsOpaqueClosed(t *testing.T) {
	body := []byte{0, 1, 2, 0xff, 0xfe}
	if verdict := Inspect("ordinary.bin", "application/octet-stream", body); verdict.ScanState == ScanClean {
		t.Fatal("ordinary upload unexpectedly admitted opaque bytes")
	}
	verdict, err := InspectKubernetesCapturedReader(KubernetesCaptureOpaque, "captured.bin", "application/octet-stream", bytes.NewReader(body), int64(len(body)))
	if err != nil || verdict.ScanState != ScanClean || verdict.PreviewState != PreviewUnavailable || verdict.MediaType != "application/octet-stream" {
		t.Fatal("captured opaque format was not kept closed", err)
	}
	for _, test := range []struct {
		name, media string
		body        []byte
	}{
		{"spoof.html", "application/octet-stream", []byte("<html>private</html>")},
		{"program.bin", "application/octet-stream", []byte("\x7fELFprivate")},
		{"script.bin", "application/octet-stream", []byte("#!/bin/sh\necho private\n")},
	} {
		got, err := InspectKubernetesCapturedReader(KubernetesCaptureOpaque, test.name, test.media, bytes.NewReader(test.body), int64(len(test.body)))
		if err != nil || got.ScanState != ScanQuarantined || got.PreviewState != PreviewBlocked {
			t.Fatal("capture bypassed active-content quarantine", test.name, err)
		}
	}
	for _, kind := range []KubernetesCapturedContentKind{KubernetesCaptureOpaque, KubernetesCaptureCopyFile, KubernetesCaptureCopyTar, "forged"} {
		got, err := InspectKubernetesCapturedReader(kind, "fake.bin", "image/png", bytes.NewReader(body), int64(len(body)))
		if err != nil || got.ScanState == ScanClean {
			t.Fatal("capture accepted a spoofed content discriminator", kind, err)
		}
	}
}
