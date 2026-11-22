package platform

import (
	"context"
	_ "embed"
	"errors"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"testing"
	"time"
)

// kubernetesCaptureHistoryDemoteSQL revokes only the disposable original actor's integration-bearing grants.
// kubernetesCaptureHistoryDemoteSQL отзывает только integration-bearing grants original actor в disposable fixture.
//
//go:embed testdata/sql/kubernetes_capture_history_demote.sql
var kubernetesCaptureHistoryDemoteSQL string

// kubernetesCaptureHistoryRestoreSQL restores exact captured grants for subsequent independent assertions.
// kubernetesCaptureHistoryRestoreSQL восстанавливает exact captured grants для последующих независимых assertions.
//
//go:embed testdata/sql/kubernetes_capture_history_restore.sql
var kubernetesCaptureHistoryRestoreSQL string

// exerciseKubernetesCaptureHistoryWithoutIntegration proves the real List/Get path before first artifact,
// while Tool denies the same actor. It grants only run.view and never invents a native or artifact result.
// exerciseKubernetesCaptureHistoryWithoutIntegration доказывает real List/Get до первого artifact,
// пока Tool отказывает тому же actor. Выдаёт только run.view, не выдумывает native либо artifact result.
func exerciseKubernetesCaptureHistoryWithoutIntegration(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, actor value.Principal, ref string, source kubernetesCaptureSource) {
	t.Helper()
	resolved, err := r.ResolvePrincipal(ctx, actor)
	if err != nil {
		t.Fatal("normalize history owner", err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal("resolve history owner", err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "capture-history-only-")
	role := execute(command.CreateAccessRole, actor, "role", nil, command.AccessRoleInput{Name: "Capture historical reader", PermissionKeys: []string{"run.view"}, AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "Disposable retained-history authority"}).AccessRole
	if role == nil {
		t.Fatal("create history reader role")
	}
	if execute(command.CreateAccessBinding, actor, "binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: current.actorRef, RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "ORGANIZATION"}}).AccessBinding == nil {
		t.Fatal("bind history reader")
	}
	admin := evidenceRetentionAdministrator(t, ctx, r.pool.Config().ConnConfig)
	rows, err := admin.Query(ctx, kubernetesCaptureHistoryDemoteSQL, current.organizationID, current.actorID)
	if err != nil {
		t.Fatal("remove integration authority from historical reader", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			t.Fatal("read demoted history grant")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil || len(ids) == 0 {
		t.Fatal("history fixture retained integration authority")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, kubernetesCaptureHistoryRestoreSQL, current.organizationID, ids); err != nil {
			t.Error("restore disposable history grants", err)
		}
	}()
	reader := actor
	reader.CallerWorkload = "control-api-gateway"
	reader.Permission = ""
	for permission, method := range controlplaneclient.ControlAPIGatewayOperations() {
		if method == cp.ManagedKubernetesCaptureService_ListManagedKubernetesCaptures_FullMethodName {
			reader.Permission = permission
		}
	}
	if reader.Permission == "" {
		t.Fatal("history operation missing from production map")
	}
	page, err := service.ListKubernetesCaptures(ctx, reader, query.KubernetesCaptureList{RunRef: source.RunRef, NodeRef: source.NodeRef, Page: query.Page{Size: 1}})
	if err != nil || len(page.Items) != 1 || page.Items[0].CaptureRef != ref || len(page.Items[0].Artifacts) != 0 || page.Next != "" {
		t.Fatal("history discovery required current integration authority", err)
	}
	reader.Permission = "platform.integrations.kubernetes.capture.get"
	view, err := service.GetKubernetesCaptureStatus(ctx, reader, query.KubernetesCaptureStatusRead{CaptureRef: ref})
	if err != nil || view.View.CaptureRef != ref {
		t.Fatal("history exact Get disagreed with List", err)
	}
	reader.Permission = "platform.integrations.kubernetes.tool.get"
	if _, err := service.GetKubernetesTool(ctx, reader, source.InvocationRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("history read accidentally granted Tool integration visibility", err)
	}
}
