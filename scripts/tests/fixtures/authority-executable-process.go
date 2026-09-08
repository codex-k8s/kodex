package main

import (
	"fmt"
	"os"
	"time"
)

// Процесс без credentials/RPC для настоящего distroless proc-readback.
func main() {
	if len(os.Args) == 2 && os.Args[1] == "double" {
		child, err := os.StartProcess(os.Args[0], []string{os.Args[0], "child"}, &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
		if err != nil {
			os.Exit(1)
		}
		defer child.Kill()
	}
	_, _ = fmt.Fprintln(os.Stdout, "ready")
	time.Sleep(120 * time.Second)
}
