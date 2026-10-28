package artifactpolicy

import (
	"bytes"
	"testing"
)

// TestRepositorySourcePurposeKeepsOrdinaryQuarantine proves that the same active text gains only
// Git-source format admission, never ordinary upload admission or a preview permission.
// TestRepositorySourcePurposeKeepsOrdinaryQuarantine проверяет, что один активный текст получает
// лишь допуск формата исходника Git, но не допуск обычной загрузки или право preview.
func TestRepositorySourcePurposeKeepsOrdinaryQuarantine(t *testing.T) {
	for _, scenario := range []struct{ name, body string }{
		{"index.html", "<!doctype html><script>alert(1)</script>"},
		{"image.svg", "<svg onload=\"alert(1)\"></svg>"},
		{"index.xhtml", "<html></html>"},
		{"start.sh", "#!/bin/sh\necho hello\n"},
		{"source.txt", "#!/usr/bin/env python3\nprint('hello')\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := []byte(scenario.body)
			ordinary := Inspect(scenario.name, "application/octet-stream", body)
			if ordinary.ScanState != ScanQuarantined || ordinary.PreviewState != PreviewBlocked {
				t.Fatal("ordinary artifact policy was weakened")
			}
			source, err := InspectRepositorySourceReader(scenario.name, bytes.NewReader(body), int64(len(body)))
			if err != nil || source.ScanState != ScanClean || source.PreviewState != PreviewUnavailable {
				t.Fatal("source format admission is missing or grants preview", source, err)
			}
		})
	}
}

// TestRepositorySourcePurposeRejectsInvalidBytes exercises active-name disguises, complete text
// validation and exact length so the specialized path cannot turn binary data into clean source.
// TestRepositorySourcePurposeRejectsInvalidBytes проверяет маскировку активным именем, весь текст
// и точный размер, чтобы отдельный путь не превращал двоичные данные в допущенные исходники.
func TestRepositorySourcePurposeRejectsInvalidBytes(t *testing.T) {
	for _, scenario := range []struct {
		name string
		body []byte
		size int64
	}{
		{"mz.html", []byte("MZ text"), 7},
		{"elf.html", []byte("\x7fELF text"), 9},
		{"archive.html", []byte("PK\x03\x04text"), 8},
		{"nul.html", []byte("<html>\x00</html>"), 14},
		{"utf8.html", []byte{0xff, 0xfe}, 2},
		{"short.html", []byte("<html>"), 7},
		{"extra.html", []byte("<html>"), 5},
		{"invalid.json", []byte("#!/bin/sh\ntrue\n"), 15},
		{"malformed.json", []byte("{broken}"), 8},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			verdict, err := InspectRepositorySourceReader(scenario.name, bytes.NewReader(scenario.body), scenario.size)
			if err == nil && verdict.ScanState == ScanClean {
				t.Fatal("invalid source admitted")
			}
		})
	}
	if _, err := InspectRepositorySourceReader("empty.html", nil, 0); err == nil {
		t.Fatal("missing reader admitted")
	}
	body := []byte("package main\n")
	verdict, err := InspectRepositorySourceReader("main.go", bytes.NewReader(body), int64(len(body)))
	if err != nil || verdict.ScanState != ScanClean || verdict.PreviewState != PreviewUnavailable {
		t.Fatal("ordinary source lost admission or acquired preview")
	}
}
