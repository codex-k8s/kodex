package stagingcrypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func privateMaterialDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMaterialGenerateRotateRetainsEveryReadKey(t *testing.T) {
	dir := privateMaterialDirectory(t)
	initial := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := GenerateFile(initial); err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(initial)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(firstBytes)
	info, _ := os.Stat(initial)
	if info.Mode().Perm() != 0o400 {
		t.Fatal("generated keyring is not owner-readonly")
	}
	first, firstSummary, err := readMaterial(initial)
	defer clearKeyring(&first)
	if err != nil || firstSummary.Revision != 1 || len(first.Keys) != 1 || first.Current != first.Keys[0].ID {
		t.Fatal("invalid generated keyring")
	}
	sum := sha256.Sum256(first.Keys[0].Material)
	if len(first.Keys[0].Material) != 32 || first.Keys[0].ID != hex.EncodeToString(sum[:]) {
		t.Fatal("key identity does not bind random material")
	}
	if err := RotateFile(initial, second, 1); err != nil {
		t.Fatal(err)
	}
	rotated, summary, err := readMaterial(second)
	defer clearKeyring(&rotated)
	if err != nil || summary.Revision != 2 || summary.Digest == firstSummary.Digest || len(rotated.Keys) != 2 || rotated.Current != rotated.Keys[1].ID || rotated.Keys[1].Generation != 2 || rotated.Keys[0].ID != first.Keys[0].ID || !bytes.Equal(rotated.Keys[0].Material, first.Keys[0].Material) {
		t.Fatal("rotation lost prior read key or generation")
	}
	readback, _ := os.ReadFile(initial)
	defer clear(readback)
	if !bytes.Equal(readback, firstBytes) {
		t.Fatal("rotation mutated input")
	}
	third := filepath.Join(dir, "third.json")
	if err := RotateFile(second, third, 2); err != nil {
		t.Fatal(err)
	}
	latest, _, err := readMaterial(third)
	defer clearKeyring(&latest)
	if err != nil || len(latest.Keys) != 3 || !bytes.Equal(latest.Keys[0].Material, first.Keys[0].Material) {
		t.Fatal("second rotation retired oldest key")
	}
}

func TestMaterialNoOverwriteAndExpectedRevision(t *testing.T) {
	dir := privateMaterialDirectory(t)
	input := filepath.Join(dir, "input.json")
	if err := GenerateFile(input); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(input)
	defer clear(before)
	if GenerateFile(input) == nil || RotateFile(input, input, 1) == nil {
		t.Fatal("existing material was overwritten")
	}
	output := filepath.Join(dir, "output.json")
	if RotateFile(input, output, 2) == nil {
		t.Fatal("wrong expected revision accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed rotation left material file")
	}
	after, _ := os.ReadFile(input)
	defer clear(after)
	if !bytes.Equal(before, after) {
		t.Fatal("failed operation modified material")
	}
}

func TestMaterialRejectsUnsafeFilesAndPaths(t *testing.T) {
	dir := privateMaterialDirectory(t)
	input := filepath.Join(dir, "input.json")
	if err := GenerateFile(input); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFile(link); err == nil {
		t.Fatal("symlink keyring accepted")
	}
	if GenerateFile(link) == nil {
		t.Fatal("symlink output overwritten")
	}
	hardlink := filepath.Join(dir, "hard.json")
	if err := os.Link(input, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFile(input); err == nil {
		t.Fatal("hard-linked keyring accepted")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(input, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFile(input); err == nil {
		t.Fatal("public keyring accepted")
	}
	if GenerateFile("relative.json") == nil {
		t.Fatal("relative output accepted")
	}
	publicDir := filepath.Join(dir, "public")
	if err := os.Mkdir(publicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if GenerateFile(filepath.Join(publicDir, "keys.json")) == nil {
		t.Fatal("public parent accepted")
	}
	alias := filepath.Join(dir, "alias")
	private := privateMaterialDirectory(t)
	if err := os.Symlink(private, alias); err != nil {
		t.Fatal(err)
	}
	if GenerateFile(filepath.Join(alias, "keys.json")) == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestMaterialRejectsCorruptionCapacityAndOverflow(t *testing.T) {
	dir := privateMaterialDirectory(t)
	for name, raw := range map[string][]byte{"malformed": []byte(`{"version":1}`), "duplicate": []byte(`{"version":1,"version":1,"revision":1,"current":"","keys":[]}`), "oversize": bytes.Repeat([]byte("x"), maximumKeyringBytes+1)} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckFile(path); err == nil {
			t.Fatal("corrupt keyring accepted")
		}
	}
	doc := keyringDocument{Version: 1, Revision: maximumKeyringKeys}
	for generation := int64(1); generation <= maximumKeyringKeys; generation++ {
		if err := appendRandomKey(&doc, generation); err != nil {
			t.Fatal(err)
		}
	}
	defer clearKeyring(&doc)
	capacity := filepath.Join(dir, "capacity.json")
	if err := writeMaterial(capacity, doc); err != nil {
		t.Fatal(err)
	}
	if RotateFile(capacity, filepath.Join(dir, "extra.json"), maximumKeyringKeys) == nil {
		t.Fatal("key retention capacity exceeded")
	}
	doc.Keys = doc.Keys[:1]
	doc.Current = doc.Keys[0].ID
	doc.Revision = math.MaxInt64
	overflow := filepath.Join(dir, "overflow.json")
	raw, _ := json.Marshal(doc)
	defer clear(raw)
	if err := os.WriteFile(overflow, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if RotateFile(overflow, filepath.Join(dir, "wrapped.json"), math.MaxInt64) == nil {
		t.Fatal("manifest revision overflowed")
	}
}
