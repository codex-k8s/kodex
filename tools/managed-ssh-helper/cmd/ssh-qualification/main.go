// Command ssh-qualification renders or validates bounded local operator records; it does not install a helper, contact SSH hosts or apply Kubernetes resources.
// Command ssh-qualification формирует либо проверяет bounded local operator records; helper не устанавливается, SSH hosts и Kubernetes resources не вызываются.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/tools/managed-ssh-helper/internal/operator"
)

// main owns cancellation and the process result, exposing only fixed diagnostics and digests rather than private target coordinates or artifact contents.
// main владеет cancellation и process result, выводя только fixed diagnostics и digests без private target coordinates либо artifact contents.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result, err := operator.Run(ctx, os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, operator.ErrRejected)
		os.Exit(1)
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		fmt.Fprintln(os.Stderr, operator.ErrRejected)
		os.Exit(1)
	}
}
