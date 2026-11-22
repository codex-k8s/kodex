// Command managed-ssh-helper serves one fixed bounded protocol frame through an operator-installed forced SSH command, without parsing command-line extensions.
// Command managed-ssh-helper обслуживает один fixed bounded protocol frame через устанавливаемую оператором forced SSH command без дополнительных command-line options.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/tools/managed-ssh-helper/internal/app"
)

// main owns signals and process exit only; rejected input or installation emits one fixed diagnostic and never prints credentials or remote file contents to stderr.
// main владеет только signals и process exit; отказ input либо installation выводит одну fixed diagnostic без credentials либо remote file contents в stderr.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
