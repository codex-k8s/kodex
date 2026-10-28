package sshcompose

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Receipt records one reserved deployment and its safe terminal evidence durably.
// Receipt устойчиво хранит один зарезервированный deploy и безопасный итог.
type Receipt struct {
	Version       int    `json:"version"`
	TargetRef     string `json:"targetRef"`
	ProjectRef    string `json:"projectRef"`
	DeploymentRef string `json:"deploymentRef"`
	EffectKey     string `json:"effectKey"`
	InputDigest   string `json:"inputDigest"`
	MergedSHA     string `json:"mergedSha"`
	TreeSHA       string `json:"treeSha"`
	BundleSHA256  string `json:"bundleSha256"`
	Image         string `json:"image"`
	Generation    int64  `json:"generation"`
	ExpiresAt     int64  `json:"expiresAt"`
	Deadline      int64  `json:"deadline"`
	State         string `json:"state"`
	HealthOK      bool   `json:"healthOk"`
	SafeErrorCode string `json:"safeErrorCode,omitempty"`
	StartedAt     int64  `json:"startedAt"`
	FinishedAt    int64  `json:"finishedAt,omitempty"`
}

// Journal owns an exclusive host lock and atomic receipt files outside releases.
// Journal владеет эксклюзивным host lock и атомарными receipt вне каталогов релиза.
type Journal struct {
	root string
	lock *os.File
}

// OpenJournal obtains a nonblocking target lock and rejects linked or foreign roots.
// OpenJournal получает неблокирующий target lock и отклоняет чужие каталоги/ссылки.
func OpenJournal(root string) (*Journal, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrorInvalid
	}
	if err := privateDirectories(root); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, ErrorInvalid
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) {
		return nil, ErrorInvalid
	}
	fd, err := syscall.Open(filepath.Join(root, "lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "journal-lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, ErrorInvalid
	}
	return &Journal{root: root, lock: file}, nil
}

// Close releases the process-owned lock after workers have joined.
// Close освобождает lock после завершения принадлежащих процессу workers.
func (journal *Journal) Close() error { return journal.lock.Close() }

// ReadReceipt observes an atomically committed receipt while another process owns the write lock.
// ReadReceipt читает атомарно сохранённый receipt, пока другой процесс владеет write lock.
func ReadReceipt(root, deploymentRef string) (Receipt, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return Receipt{}, ErrorInvalid
	}
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Receipt{}, ErrorInvalid
		}
		if current == root {
			metadata, ok := info.Sys().(*syscall.Stat_t)
			if !ok || metadata.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0700 {
				return Receipt{}, ErrorInvalid
			}
		}
	}
	return (&Journal{root: root}).Read(deploymentRef)
}

// Read returns the exact persisted deployment receipt without executing anything.
// Read возвращает точный сохранённый receipt, не выполняя внешних действий.
func (journal *Journal) Read(deploymentRef string) (Receipt, error) {
	if !refPattern.MatchString(deploymentRef) {
		return Receipt{}, ErrorInvalid
	}
	fd, err := syscall.Open(filepath.Join(journal.root, deploymentRef+".json"), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Receipt{}, err
	}
	file := os.NewFile(uintptr(fd), "receipt")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
		return Receipt{}, ErrorInvalid
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) || metadata.Nlink != 1 || info.Mode().Perm() != 0600 {
		return Receipt{}, ErrorInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if Decode(raw, &receipt, 16384) != nil || receipt.Version != ProtocolVersion || receipt.DeploymentRef != deploymentRef {
		return Receipt{}, ErrorInvalid
	}
	return receipt, nil
}

// Reserve persists the no-replay barrier before source extraction or Compose execution.
// Reserve сохраняет запрет replay до извлечения исходников либо запуска Compose.
func (journal *Journal) Reserve(permit Permit, image string) (Receipt, bool, error) {
	if permit.Purpose != "DEPLOY" || permit.InvocationRef != permit.DeploymentRef || permit.InputDigest != permit.DeploymentInputDigest {
		return Receipt{}, false, ErrorInvalid
	}
	previous, err := journal.Read(permit.DeploymentRef)
	if err == nil {
		if !receiptMatches(previous, permit) {
			return Receipt{}, false, ErrorInvalid
		}
		return previous, false, nil
	}
	if !os.IsNotExist(err) {
		return Receipt{}, false, err
	}
	// Receipts are never removed automatically because each one is a no-replay barrier.
	// Receipts не удаляются автоматически: каждый является устойчивым запретом replay.
	entries, err := os.ReadDir(journal.root)
	if err != nil || len(entries) >= 1025 {
		return Receipt{}, false, ErrorInvalid
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		other, readErr := journal.Read(strings.TrimSuffix(entry.Name(), ".json"))
		if readErr != nil {
			return Receipt{}, false, ErrorInvalid
		}
		if other.EffectKey == permit.EffectKey {
			if other.TargetRef != permit.Target.Ref || other.ProjectRef != permit.Target.ProjectRef || other.MergedSHA != permit.MergedSHA || other.TreeSHA != permit.TreeSHA || other.BundleSHA256 != permit.BundleSHA256 || other.Image != image {
				return Receipt{}, false, ErrorInvalid
			}
			return other, false, nil
		}
	}
	receipt := Receipt{Version: ProtocolVersion, TargetRef: permit.Target.Ref, ProjectRef: permit.Target.ProjectRef,
		DeploymentRef: permit.DeploymentRef, EffectKey: permit.EffectKey, InputDigest: permit.DeploymentInputDigest, MergedSHA: permit.MergedSHA,
		TreeSHA: permit.TreeSHA, BundleSHA256: permit.BundleSHA256, Image: image, Generation: permit.Generation, ExpiresAt: permit.ExpiresAt,
		Deadline: permit.Deadline, State: "RESERVED", StartedAt: time.Now().Unix()}
	if err := journal.write(receipt); err != nil {
		return Receipt{}, false, err
	}
	return receipt, true, nil
}

// Renew accepts only a newer owner expiry for the same generation and exact input.
// Renew принимает новый owner expiry только той же generation и точного input.
func (journal *Journal) Renew(receipt Receipt, permit Permit) (Receipt, error) {
	if permit.Purpose != "RENEW" || !receiptMatches(receipt, permit) || permit.InvocationRef != permit.DeploymentRef ||
		permit.Generation != receipt.Generation || permit.ExpiresAt < receipt.ExpiresAt || permit.Deadline != receipt.Deadline ||
		(receipt.State != "RESERVED" && receipt.State != "RUNNING") {
		return Receipt{}, ErrorInvalid
	}
	receipt.ExpiresAt = permit.ExpiresAt
	return receipt, journal.write(receipt)
}

// Complete records terminal state without treating an uncertain cancellation as rollback.
// Complete фиксирует terminal state, не выдавая неопределённую отмену за rollback.
func (journal *Journal) Complete(receipt Receipt, state, code string, healthy bool) (Receipt, error) {
	switch state {
	case "RUNNING":
	case "SUCCEEDED", "FAILED", "CANCELLED", "UNKNOWN_OUTCOME":
		receipt.FinishedAt = time.Now().Unix()
	default:
		return Receipt{}, ErrorInvalid
	}
	if receipt.State == "SUCCEEDED" || receipt.State == "FAILED" || receipt.State == "CANCELLED" || receipt.State == "UNKNOWN_OUTCOME" {
		return Receipt{}, ErrorInvalid
	}
	receipt.State, receipt.SafeErrorCode, receipt.HealthOK = state, code, healthy
	return receipt, journal.write(receipt)
}

// receiptMatches compares immutable owner pins before replay, renewal or status use.
// receiptMatches сверяет immutable owner pins до replay, renew либо status.
func receiptMatches(receipt Receipt, permit Permit) bool {
	return receipt.TargetRef == permit.Target.Ref && receipt.ProjectRef == permit.Target.ProjectRef && receipt.DeploymentRef == permit.DeploymentRef &&
		receipt.EffectKey == permit.EffectKey && receipt.InputDigest == permit.DeploymentInputDigest && receipt.MergedSHA == permit.MergedSHA &&
		receipt.TreeSHA == permit.TreeSHA && receipt.BundleSHA256 == permit.BundleSHA256
}

// Matches exposes immutable receipt validation for a separately authorized status read.
// Matches предоставляет проверку immutable receipt для отдельно разрешённого status read.
func (receipt Receipt) Matches(permit Permit) bool { return receiptMatches(receipt, permit) }

// write atomically replaces a private receipt and syncs both data and its directory.
// write атомарно заменяет private receipt и синхронизирует данные с каталогом.
func (journal *Journal) write(receipt Receipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return ErrorInvalid
	}
	file, err := os.CreateTemp(journal.root, ".receipt-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, filepath.Join(journal.root, receipt.DeploymentRef+".json")); err != nil {
		return err
	}
	directory, err := os.Open(journal.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
