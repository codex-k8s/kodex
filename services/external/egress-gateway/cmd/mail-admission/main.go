// This command generates the sole mail admission artifact from the shared source.
// Команда генерирует единственный почтовый admission-артефакт из общего источника.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

// main emits the exact admission resources as a Kubernetes List on stdout.
// main выдаёт точные admission-ресурсы в виде Kubernetes List в stdout.
func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "mail admission generator takes no arguments")
		os.Exit(1)
	}
	policy, binding := mailpolicy.PublicationAdmissionResources()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{policy, binding}}); err != nil {
		fmt.Fprintln(os.Stderr, "mail admission generation failed")
		os.Exit(1)
	}
}
