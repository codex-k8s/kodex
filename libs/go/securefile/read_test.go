package securefile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadAcceptsExactReadOnlyModes checks all three explicit file modes,
// reading a regular nonempty fixture for each without a projected exception.
// RU: TestReadAcceptsExactReadOnlyModes проверяет все три точных режима на
// непустом обычном файле без исключения для projected token.
func TestReadAcceptsExactReadOnlyModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o400, 0o440, 0o444} {
		mode := mode
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte("value"), mode); err != nil {
				t.Fatal(err)
			}
			value, err := Read(path, 16)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if string(value) != "value" {
				t.Fatalf("Read() returned unexpected value length %d", len(value))
			}
		})
	}
}

// TestReadRejectsWritableExecutableAndOwnerUnreadableModes verifies that
// deviations from the approved modes fail even when the process can open the file.
// RU: TestReadRejectsWritableExecutableAndOwnerUnreadableModes проверяет отказ
// для иных режимов, даже когда процесс технически может открыть файл.
func TestReadRejectsWritableExecutableAndOwnerUnreadableModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o000, 0o004, 0o040, 0o404, 0o500, 0o600, 0o640, 0o644, 0o660} {
		mode := mode
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(path, 16); err == nil {
				t.Fatalf("Read() accepted mode %04o", mode)
			}
		})
	}
}

// TestProjectedServiceAccountTokenRejectsProcessOwnedWritableFile requires a
// writable 0640 token owned by the caller to remain forbidden.
// RU: TestProjectedServiceAccountTokenRejectsProcessOwnedWritableFile требует
// отказа для записываемого токена 0640, принадлежащего самому процессу.
func TestProjectedServiceAccountTokenRejectsProcessOwnedWritableFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("value"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProjectedServiceAccountToken(path, 16); err == nil {
		t.Fatal("projected token reader accepted a process-owned writable file")
	}
}

// TestProjectedServiceAccountTokenModeRequiresRootOwnerAndNonRootProcess
// exercises the exact 0640 owner/EUID/read-only-mount matrix without chmod
// or a privileged test process.
// RU: TestProjectedServiceAccountTokenModeRequiresRootOwnerAndNonRootProcess
// проверяет матрицу владельца, EUID и read-only mount для 0640 без chmod и
// запуска теста с повышенными правами.
func TestProjectedServiceAccountTokenModeRequiresRootOwnerAndNonRootProcess(t *testing.T) {
	t.Parallel()
	info := syntheticFileInfo{mode: 0o640, system: &syscall.Stat_t{Uid: 0}}
	if !isSafeMode(info, true, 29008, false) {
		t.Fatal("root-owned projected token was rejected for a non-root workload")
	}
	if isSafeMode(info, true, 0, false) {
		t.Fatal("root process accepted its writable projected token as read-only")
	}
	info.system = &syscall.Stat_t{Uid: 29008}
	if isSafeMode(info, true, 29008, false) {
		t.Fatal("process-owned projected token was accepted")
	}
	if !isSafeMode(info, true, 29008, true) {
		t.Fatal("process-owned projected token on a read-only mount was rejected")
	}
}

// TestReadWithinRejectsSymlinkEscape points a file outside the selected root
// and requires rejection before any target bytes are returned.
// RU: TestReadWithinRejectsSymlinkEscape направляет ссылку за выбранный
// корень и требует отказа до возврата байтов целевого файла.
func TestReadWithinRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(outside, []byte("value"), 0o400); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "credential")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWithin(root, link, 16); err == nil {
		t.Fatal("ReadWithin() accepted a symlink outside root")
	}
}

// TestReadAcceptsProjectedSymlinkInsideMount models an internal Kubernetes
// ..data target and requires the approved read-only file to remain readable.
// RU: TestReadAcceptsProjectedSymlinkInsideMount воспроизводит внутреннюю
// цель ..data Kubernetes и требует чтения допустимого read-only файла.
func TestReadAcceptsProjectedSymlinkInsideMount(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	versioned := filepath.Join(root, "..data")
	if err := os.Mkdir(versioned, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(versioned, "credential")
	if err := os.WriteFile(target, []byte("value"), 0o444); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "credential")
	if err := os.Symlink(filepath.Join("..data", "credential"), link); err != nil {
		t.Fatal(err)
	}
	value, err := Read(link, 16)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(value) != "value" {
		t.Fatalf("Read() returned unexpected value length %d", len(value))
	}
}

// TestReadRejectsUnboundedAndOversizedFiles checks both a zero size limit and
// an existing file larger than the caller's maximum.
// RU: TestReadRejectsUnboundedAndOversizedFiles проверяет нулевой лимит
// размера и существующий файл, превышающий заданный максимум.
func TestReadRejectsUnboundedAndOversizedFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("value"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 0); err == nil {
		t.Fatal("Read() accepted a zero size boundary")
	}
	if _, err := Read(path, 4); err == nil {
		t.Fatal("Read() accepted an oversized file")
	}
}

// syntheticFileInfo supplies controlled mode and ownership metadata for the
// projected-token matrix without depending on the test runner's UID.
// RU: syntheticFileInfo задаёт режим и владельца для матрицы projected token
// независимо от UID процесса, исполняющего тесты.
type syntheticFileInfo struct {
	mode   os.FileMode
	system any
}

// Name returns a fixed fixture label; the mode policy never relies on it.
// RU: Name возвращает постоянное имя fixture; политика режима от него не зависит.
func (info syntheticFileInfo) Name() string { return "token" }

// Size returns a nonempty fixture length for os.FileInfo compatibility.
// RU: Size возвращает непустую длину fixture для совместимости с os.FileInfo.
func (info syntheticFileInfo) Size() int64 { return 1 }

// Mode exposes the injected permission bits tested by isSafeMode.
// RU: Mode выдаёт заданные биты доступа, проверяемые isSafeMode.
func (info syntheticFileInfo) Mode() os.FileMode { return info.mode }

// ModTime returns an unused zero timestamp for the file-info contract.
// RU: ModTime возвращает неиспользуемую нулевую отметку времени для контракта файла.
func (info syntheticFileInfo) ModTime() time.Time { return time.Time{} }

// IsDir marks the fixture as a regular-file candidate rather than a directory.
// RU: IsDir отмечает fixture как кандидат на обычный файл, а не каталог.
func (info syntheticFileInfo) IsDir() bool { return false }

// Sys supplies the injected Stat_t used to test UID-dependent acceptance.
// RU: Sys выдаёт заданный Stat_t для проверки допуска в зависимости от UID.
func (info syntheticFileInfo) Sys() any { return info.system }
