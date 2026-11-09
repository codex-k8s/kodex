// Command policycompatgen emits exact immutable predecessor capabilities for the resource-policy migration; no live environment or credential is read.
// Команда policycompatgen выводит exact immutable predecessor capabilities для миграции resource policy; live environment и credentials не читаются.
package main

import (
	_ "embed"
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// legacyPolicySQL is the embedded immutable SQL projection; generation never executes SQL or reads deployment inputs.
// legacyPolicySQL — встроенная immutable SQL projection; генерация не исполняет SQL и не читает deployment inputs.
//
//go:embed sql/legacy_policy_function.sql
var legacyPolicySQL string

// legacyPolicyRow contains only validated local registry pins and a bounded JSON capability projection.
// legacyPolicyRow содержит лишь проверенные local registry pins и bounded JSON capability projection.
type legacyPolicyRow struct {
	Key, CurrentVersion, CurrentDigest, Version, Digest, Capabilities string
}

// main resolves each local frozen package through the executable registry before emitting SQL; an unknown snapshot or altered successor closes generation.
// main разрешает каждый frozen package через executable registry до вывода SQL; неизвестный snapshot либо изменённый successor закрывает генерацию.
func main() {
	shipped, err := integrationpackage.LoadShipped()
	if err != nil {
		panic(err)
	}
	files, err := filepath.Glob("compatibility/*.yaml")
	if err != nil || len(files) == 0 {
		panic("compatibility inputs unavailable")
	}
	rows := make([]legacyPolicyRow, 0, len(files))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		previous, err := integrationpackage.Parse(raw)
		if err != nil {
			panic(err)
		}
		current := shipped[previous.Metadata.Key]
		if _, ok := integrationpackage.ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest); !ok {
			panic("unregistered compatibility snapshot")
		}
		projection := make([]map[string]any, 0, len(previous.Spec.Capabilities))
		for _, capability := range previous.Spec.Capabilities {
			projection = append(projection, map[string]any{"key": capability.Key, "name": capability.Name, "operation": capability.Operation, "risk": capability.Risk, "approvalPolicy": capability.ApprovalPolicy, "allowedApprovalPolicies": capability.AllowedApprovalPolicies, "resourceKind": capability.ResourceScope.Kind})
		}
		raw, err = json.Marshal(projection)
		if err != nil || strings.Contains(string(raw), "$caps$") {
			panic("invalid capabilities")
		}
		rows = append(rows, legacyPolicyRow{current.Metadata.Key, current.Metadata.Version, current.Digest, previous.Metadata.Version, previous.Digest, string(raw)})
	}
	_, statement, ok := strings.Cut(legacyPolicySQL, "\n")
	if !ok {
		panic("SQL template header unavailable")
	}
	if err := template.Must(template.New("legacy-policy").Parse(statement)).Execute(os.Stdout, rows); err != nil {
		panic(err)
	}
}
