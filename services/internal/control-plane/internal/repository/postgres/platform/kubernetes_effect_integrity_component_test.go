package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertKubernetesPlanObservationIntegrity injects valid but inconsistent immutable observations
// only through the existing exact disposable administrator. Rejoin must reject the changed original
// schema/body/namespace evidence even while coordinates, proposal field and plan hash stay unchanged.
// assertKubernetesPlanObservationIntegrity меняет valid immutable observations только через existing
// exact disposable administrator. Rejoin отвергает смену original schema/body/namespace evidence при
// неизменных coordinates, proposal field и plan hash.
func assertKubernetesPlanObservationIntegrity(t *testing.T, ctx context.Context, fixture kubernetesSetupOwnerFixture, broker value.Principal, binding value.ManagedKubernetesBinding, prepare command.ManagedKubernetesPrepareChildren, childRef string, original managedkubernetes.ChildCandidate) {
	t.Helper()
	admin := evidenceRetentionAdministrator(t, ctx, fixture.pool.Config().ConnConfig)
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	// replaceObservation changes one fixture row under the table lock and restores its immutable guard
	// before committing, so no runtime write is permitted during the corruption counterfactual.
	// replaceObservation меняет fixture row под table lock и восстанавливает immutable guard до commit,
	// не допуская runtime write во время corruption counterfactual.
	replaceObservation := func(raw []byte) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, "ALTER TABLE control_plane.kubernetes_execution_children DISABLE TRIGGER kubernetes_execution_child_immutable"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE control_plane.kubernetes_execution_children SET observation_json=$1::jsonb WHERE ref=$2", string(raw), childRef); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "ALTER TABLE control_plane.kubernetes_execution_children ENABLE TRIGGER kubernetes_execution_child_immutable"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"schema", "body", "namespace"} {
		changed := original
		switch field {
		case "schema":
			changed.RegisteredSchemaDigest = strings.Repeat("c", 64)
		case "body":
			changed.ObservedResourceDigest = strings.Repeat("d", 64)
		case "namespace":
			changed.NamespaceUID = "30000000-0000-4000-8000-000000000001"
		}
		damaged, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		replaceObservation(damaged)
		_, readErr := fixture.repository.PrepareManagedKubernetesChildren(ctx, broker, binding, prepare)
		replaceObservation(raw)
		if !errors.Is(readErr, errs.ErrUnavailable) {
			t.Fatal("changed original observation retained the immutable plan digest", field, readErr)
		}
	}
}
