package controlplane

import (
	"embed"
	"fmt"
)

//go:embed sql/*.sql
var queryFiles embed.FS

func query(name string) string {
	raw, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic(fmt.Sprintf("embedded control-plane query %q is missing", name))
	}
	return string(raw)
}
