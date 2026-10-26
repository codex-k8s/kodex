// Command ssh-compose-helper serves the restricted owner-signed SSH stdin protocol.
// Команда ssh-compose-helper обслуживает ограниченный owner-signed протокол SSH stdin.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// configuration is a root-owned public target/trust document containing no private keys.
// configuration — root-owned документ target/trust, не содержащий private keys.
type configuration struct {
	Target        sshcompose.Target `json:"target"`
	RootDirectory string            `json:"rootDirectory"`
	OwnerKeys     map[string]string `json:"ownerKeys"`
}

// main owns the root lifecycle and emits only a fixed safe diagnostic on failure.
// main владеет корневым lifecycle и выдаёт фиксированную диагностику при отказе.
func main() {
	configPath := flag.String("config", "/etc/kodex-ssh-compose.json", "public root-owned target configuration")
	boot := flag.Bool("boot", false, "start only a durably committed exact container")
	flag.Parse()
	if flag.NArg() != 0 || os.Geteuid() == 0 || (!*boot && os.Getenv("SSH_ORIGINAL_COMMAND") != "kodex-ssh-compose-v1") {
		_, _ = fmt.Fprintln(os.Stderr, "SSH Compose helper configuration rejected")
		os.Exit(1)
	}
	background := context.Background()
	ctx, stop := signal.NotifyContext(background, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configPath, *boot); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "SSH Compose operation rejected")
		os.Exit(1)
	}
}

// run verifies public configuration ownership and then joins the complete helper lifecycle.
// run проверяет владение public config и ожидает полный lifecycle helper.
func run(ctx context.Context, name string, boot bool) error {
	fd, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return sshcompose.ErrorInvalid
	}
	file := os.NewFile(uintptr(fd), "helper-config")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 || info.Mode().Perm()&0022 != 0 {
		return sshcompose.ErrorInvalid
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Uid != 0 || metadata.Nlink != 1 {
		return sshcompose.ErrorInvalid
	}
	var config configuration
	raw, readErr := io.ReadAll(io.LimitReader(file, 16385))
	if readErr != nil || sshcompose.Decode(raw, &config, 16384) != nil || config.Target.Validate() != nil || len(config.OwnerKeys) == 0 || len(config.OwnerKeys) > 4 {
		return sshcompose.ErrorInvalid
	}
	keys := map[string]ed25519.PublicKey{}
	for id, encoded := range config.OwnerKeys {
		key, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(key) != ed25519.PublicKeySize || sshcompose.Digest(key) != id {
			return sshcompose.ErrorInvalid
		}
		keys[id] = ed25519.PublicKey(key)
	}
	helper := sshcompose.Helper{Target: config.Target, OwnerKeys: keys, RootDirectory: config.RootDirectory, Runner: sshcompose.DockerRunner{}}
	if boot {
		return helper.Boot(ctx)
	}
	input, err := protocolInput(os.Stdin)
	if err != nil {
		return err
	}
	// Serve владеет новым descriptor и закрывает/join его; исходный fd0 не усыновляется.
	return helper.Serve(ctx, input, os.Stdout)
}

// protocolInput owns a duplicate pollable pipe/socket descriptor so Close interrupts
// the control reader even while the SSH peer keeps stdin open awaiting a receipt.
// It never reopens /dev/stdin, adopts fd0, or falls back to a blocking reader.
// protocolInput владеет duplicate pollable pipe/socket descriptor, поэтому Close
// прерывает control reader, даже когда SSH peer ждёт receipt с открытым stdin.
// /dev/stdin не переоткрывается, fd0 не усыновляется, blocking fallback отсутствует.
func protocolInput(source *os.File) (*os.File, error) {
	if source == nil {
		return nil, sshcompose.ErrorInvalid
	}
	info, err := source.Stat()
	if err != nil || info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return nil, sshcompose.ErrorInvalid
	}
	descriptor, err := syscall.Dup(int(source.Fd()))
	if err != nil {
		return nil, sshcompose.ErrorInvalid
	}
	syscall.CloseOnExec(descriptor)
	// O_NONBLOCK принадлежит shared open-file description. CLI после этой точки
	// читает только duplicate; original os.Stdin остаётся открытым до выхода процесса.
	if syscall.SetNonblock(descriptor, true) != nil {
		_ = syscall.Close(descriptor)
		return nil, sshcompose.ErrorInvalid
	}
	input := os.NewFile(uintptr(descriptor), "ssh-compose-protocol-input")
	if input == nil {
		_ = syscall.Close(descriptor)
		return nil, sshcompose.ErrorInvalid
	}
	if input.SetReadDeadline(time.Time{}) != nil {
		_ = input.Close()
		return nil, sshcompose.ErrorInvalid
	}
	return input, nil
}
