// Command ssh-compose-owner-keygen creates operator-owned signing files without printing key bytes.
// Команда ssh-compose-owner-keygen создаёт operator-owned signing files без вывода key bytes.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// main requires two explicit new files and emits only a fixed completion diagnostic.
// main требует два явных новых файла и выдаёт только fixed completion diagnostic.
func main() {
	privatePath := flag.String("private-file", "", "new private signing file")
	publicPath := flag.String("public-file", "", "new public trust JSON file")
	flag.Parse()
	if flag.NArg() != 0 || *privatePath == *publicPath || !filepath.IsAbs(*privatePath) || !filepath.IsAbs(*publicPath) || run(*privatePath, *publicPath) != nil {
		_, _ = fmt.Fprintln(os.Stderr, "SSH Compose owner key generation rejected")
		os.Exit(1)
	}
	fmt.Println("SSH Compose owner signing files created")
}

// run writes fresh private and public files with exclusive no-follow creation and fsync.
// run сохраняет fresh private/public files через exclusive no-follow creation и fsync.
func run(privatePath, publicPath string) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return sshcompose.ErrorInvalid
	}
	defer clear(private)
	encoded, err := json.Marshal(map[string]string{sshcompose.Digest(public): base64.StdEncoding.EncodeToString(public)})
	if err != nil {
		return sshcompose.ErrorInvalid
	}
	if err := writeNew(privatePath, private); err != nil {
		return err
	}
	return writeNew(publicPath, encoded)
}

// writeNew refuses replacement, links and nonprivate parent directories before storing signing bytes.
// writeNew отклоняет replacement, links и nonprivate parent directories до записи signing bytes.
func writeNew(name string, raw []byte) error {
	parent, err := os.Lstat(filepath.Dir(name))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return sshcompose.ErrorInvalid
	}
	metadata, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != uint32(os.Geteuid()) {
		return sshcompose.ErrorInvalid
	}
	fd, err := syscall.Open(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return sshcompose.ErrorInvalid
	}
	file := os.NewFile(uintptr(fd), "signing-output")
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
