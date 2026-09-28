// Команда генерирует admission artifact из общего source.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "integration admission generator takes no arguments")
		os.Exit(1)
	}
	policy, binding := integrationegresspolicy.PublicationAdmissionResources()
	boundary, boundaryBinding := integrationegresspolicy.CreationBoundaryResources()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{policy, binding, boundary, boundaryBinding}}); err != nil {
		fmt.Fprintln(os.Stderr, "integration admission generation failed")
		os.Exit(1)
	}
}
