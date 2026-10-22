package main

import (
	"fmt"
	"os"
	"time"
)

// main provides an RPC-free process that reads no credentials for isolated proc-readback tests. It prints ready,
// sleeps for two minutes and optionally starts a same-binary child; deferred Kill is best effort on
// normal return, not a wait/join contract or cleanup guarantee after abrupt termination.
// main создаёт не читающий credentials и не вызывающий RPC процесс для изолированного proc-readback.
// Он пишет ready, ждёт две минуты и опционально создаёт child из того же binary; deferred Kill —
// best effort при нормальном выходе, не wait/join contract и не гарантия очистки при резком завершении.
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
