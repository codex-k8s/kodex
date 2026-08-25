package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

func TestRoleImageInputIsDeterministicAndSourceBound(t *testing.T) {
	source := "0123456789abcdef0123456789abcdef01234567"
	first, sourceDigest, err := roleImageInput(source)
	if err != nil {
		t.Fatalf("create first input: %v", err)
	}
	second, secondSourceDigest, err := roleImageInput(source)
	if err != nil {
		t.Fatalf("create second input: %v", err)
	}
	if !bytes.Equal(first, second) || sourceDigest != secondSourceDigest {
		t.Fatal("role image input is not deterministic")
	}
	expected := sha256.Sum256([]byte(source))
	if sourceDigest != hex.EncodeToString(expected[:]) {
		t.Fatal("source digest does not bind the exact commit")
	}
	reader := tar.NewReader(bytes.NewReader(first))
	header, err := reader.Next()
	if err != nil || header.Name != ".kodex/source.sha256" ||
		!header.FileInfo().Mode().IsRegular() {
		t.Fatalf("unexpected input entry: %#v, %v", header, err)
	}
	value, err := io.ReadAll(reader)
	if err != nil || string(value) != sourceDigest {
		t.Fatal("source binding payload is invalid")
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("unexpected additional entry: %v", err)
	}
}
