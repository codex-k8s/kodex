// Валидатор не загружает credentials и не выполняет RPC: только закрытый stdin.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

var invalidInventory = errors.New("image tool inventory is invalid")

func main() {
	if err := validate(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, invalidInventory)
		os.Exit(1)
	}
}

func validate(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || (args[0] != "capture-manifest" && args[0] != "inventory") {
		return invalidInventory
	}
	raw, err := io.ReadAll(io.LimitReader(input, runtimecontract.MaximumImageInventoryBytes+1))
	if err != nil {
		return invalidInventory
	}
	defer clear(raw)
	if args[0] == "inventory" {
		if _, err := runtimecontract.DecodeImageToolInventory(raw); err != nil {
			return invalidInventory
		}
		return nil
	}
	if _, err := runtimecontract.DecodeImageToolManifest(raw); err != nil {
		return invalidInventory
	}
	if count, err := output.Write(raw); err != nil || count != len(raw) {
		return invalidInventory
	}
	return nil
}
