package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	transport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"google.golang.org/protobuf/proto"
)

// assertSSHOwnerGatewayClaim sends the actual committed owner claim through the production mapper and an obligatory independent gateway consumer; no synthetic credential or hand-built wire response replaces the producer.
// assertSSHOwnerGatewayClaim передаёт actual committed owner claim через production mapper и обязательный independent gateway consumer; synthetic credential либо вручную собранный wire response не заменяют producer.
func assertSSHOwnerGatewayClaim(t *testing.T, item map[string]any, instance string) {
	t.Helper()
	path := os.Getenv("KODEX_SSH_GATEWAY_NATIVE_CONSUMER_BINARY")
	expected := os.Getenv("KODEX_SSH_GATEWAY_NATIVE_CONSUMER_SHA256")
	if !managedssh.Digest(expected) || !managedssh.OwnerReference(instance) {
		t.Fatal("native SSH gateway consumer is required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o500 || info.Size() < 1 || info.Size() > 256<<20 {
		t.Fatal("native SSH gateway consumer file is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("open native SSH gateway consumer")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		t.Fatal("native SSH gateway consumer changed")
	}
	stat, ok := opened.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		t.Fatal("native SSH gateway consumer ownership is invalid")
	}
	hash := sha256.New()
	if count, err := io.Copy(hash, io.LimitReader(file, (256<<20)+1)); err != nil || count != opened.Size() || hex.EncodeToString(hash.Sum(nil)) != expected {
		t.Fatal("native SSH gateway consumer digest changed")
	}
	raw, err := proto.Marshal(transport.CastIntegrationInvocationClaim(item))
	if err != nil || len(raw) == 0 || len(raw) > 512<<10 {
		t.Fatal("encode actual SSH owner claim")
	}
	defer clear(raw)
	directory := t.TempDir()
	if os.Chmod(directory, 0o700) != nil {
		t.Fatal("protect native SSH gateway consumer directory")
	}
	receipt, err := os.OpenFile(directory+"/receipt", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create private SSH gateway consumer receipt")
	}
	defer receipt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	// Executing the inherited open file binds Run to the hashed inode, even if its original path is replaced before the child starts.
	// Запуск inherited open file связывает Run с hashed inode, даже при подмене исходного path до старта child.
	child := exec.CommandContext(ctx, "/proc/self/fd/3", "-test.run=^TestSSHActualOwnerClaimConsumer$", "-test.count=1", "-test.timeout=15s")
	child.ExtraFiles = []*os.File{file, receipt}
	child.Env = []string{"TMPDIR=" + directory, "GOMAXPROCS=2", "KODEX_SSH_NATIVE_CLAIM_INSTANCE=" + instance}
	child.Stdin = bytes.NewReader(raw)
	child.WaitDelay = time.Second
	if child.Run() != nil {
		t.Fatal("actual SSH owner claim failed the independent gateway consumer")
	}
	inputDigest := sha256.Sum256(raw)
	expectedReceipt := "SSH_CLAIM_VALIDATED " + hex.EncodeToString(inputDigest[:]) + "\n"
	observed, err := receipt.Stat()
	if err != nil || observed.Size() != int64(len(expectedReceipt)) {
		t.Fatal("native SSH gateway consumer did not validate the actual frame")
	}
	if _, err := receipt.Seek(0, io.SeekStart); err != nil {
		t.Fatal("read native SSH gateway consumer receipt")
	}
	ack, err := io.ReadAll(io.LimitReader(receipt, 128))
	if err != nil || string(ack) != expectedReceipt {
		t.Fatal("native SSH gateway consumer receipt is invalid")
	}
}
