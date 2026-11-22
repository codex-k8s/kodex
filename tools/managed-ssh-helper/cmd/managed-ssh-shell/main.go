// Command managed-ssh-shell is the root-installed login shell for an exclusive managed account; it accepts no general shell language or arbitrary program.
// Command managed-ssh-shell является root-installed login shell выделенной managed account; general shell language либо arbitrary program он не принимает.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/codex-k8s/kodex/tools/managed-ssh-helper/internal/helper"
)

// main bounds deployment validation and emits only a fixed rejection; successful execution replaces this process with the verified helper inode and a minimal environment.
// main ограничивает deployment validation и выводит только fixed rejection; successful execution заменяет process проверенным helper inode с минимальным environment.
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if helper.ExecManagedAccountShell(ctx, os.Args[1:]) != nil {
		fmt.Fprintln(os.Stderr, helper.ErrRejected)
		os.Exit(1)
	}
}
