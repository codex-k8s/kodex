package httptransport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Структурная полнота дополняет, но не заменяет поведенческие route/authority tests.
func TestPublicRPCSurfaceHasHTTPConsumer(t *testing.T) {
	used := map[string]bool{}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			client, ok := selector.X.(*ast.SelectorExpr)
			if ok && (client.Sel.Name == "Query" || client.Sel.Name == "Command" || client.Sel.Name == "Assistant") {
				used[selector.Sel.Name] = true
			}
			return true
		})
	}
	profile := controlplaneclient.ControlAPIGatewayOperations()
	registered := map[string]bool{}
	for _, method := range profile {
		registered[method] = true
	}
	for _, name := range []protoreflect.Name{"PlatformQueryService", "PlatformCommandService", "SystemAssistantService"} {
		service := cp.File_controlplane_v1_control_plane_proto.Services().ByName(name)
		for i := 0; i < service.Methods().Len(); i++ {
			method := service.Methods().Get(i)
			full := "/" + string(service.FullName()) + "/" + string(method.Name())
			if !registered[full] {
				t.Errorf("public RPC lacks authority profile: %s", full)
			}
			if method.Name() == "GetPlatformEventCursor" {
				raw, err := os.ReadFile("../websocket/server.go")
				if err != nil || !strings.Contains(string(raw), ".GetPlatformEventCursor(") {
					t.Error("event cursor has no websocket consumer")
				}
				continue
			}
			if !used[string(method.Name())] {
				t.Errorf("public RPC lacks handwritten HTTP consumer: %s", full)
			}
		}
	}
	for _, forbidden := range []string{"ResolveEmailAuthorization", "ReportEmailEffectReceipt", "ResolveEmailReconciliation", "ClaimExecution", "ResolveTranscriptionCredentialProjection"} {
		if used[forbidden] {
			t.Errorf("worker RPC exposed to browser: %s", forbidden)
		}
	}
}
