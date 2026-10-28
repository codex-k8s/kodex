package sshcompose

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureBootRunner exposes bounded synthetic Docker evidence and records the exact start command.
// fixtureBootRunner предоставляет синтетический Docker readback и учитывает точную команду запуска.
type fixtureBootRunner struct {
	evidence  ContainerEvidence
	arguments []string
}

// Rootless models the already admitted rootless daemon without host operations.
// Rootless моделирует разрешённый rootless daemon без операций на хосте.
func (*fixtureBootRunner) Rootless(context.Context) bool { return true }

// Evidence returns only the fixture container; validation remains in the production helper.
// Evidence возвращает fixture container, оставляя проверку production helper.
func (runner *fixtureBootRunner) Evidence(context.Context, string) (ContainerEvidence, error) {
	return runner.evidence, nil
}

// Run records the fixed start command without invoking Docker.
// Run сохраняет фиксированную команду запуска без вызова Docker.
func (runner *fixtureBootRunner) Run(_ context.Context, arguments []string) error {
	runner.arguments = append([]string(nil), arguments...)
	return nil
}

// TestBootRequiresCommittedExactContainer prevents reboot from resuming unknown effects or substituted state.
// TestBootRequiresCommittedExactContainer запрещает reboot возобновлять неизвестные эффекты или подменённое состояние.
func TestBootRequiresCommittedExactContainer(t *testing.T) {
	for _, scenario := range []string{"committed", "reserved", "running", "unknown", "foreign-project", "foreign-volume", "writable-source", "foreign-image", "resources"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			permit := fixturePermit(t, fixtureBundle(t))
			journal, err := OpenJournal(filepath.Join(root, "journal"))
			if err != nil {
				t.Fatal(err)
			}
			receipt, _, err := journal.Reserve(permit, permit.Target.RuntimeImage)
			if err != nil {
				t.Fatal(err)
			}
			state := "SUCCEEDED"
			switch scenario {
			case "reserved":
				state = ""
			case "running":
				state = "RUNNING"
			case "unknown":
				state = "UNKNOWN_OUTCOME"
			}
			if state != "" {
				if _, err = journal.Complete(receipt, state, "", state == "SUCCEEDED"); err != nil {
					t.Fatal(err)
				}
			}
			if err = journal.Close(); err != nil {
				t.Fatal(err)
			}
			project := "kodex_" + Digest([]byte(permit.Target.ProjectRef + "\x00" + permit.Target.Ref))[:16]
			runner := &fixtureBootRunner{evidence: ContainerEvidence{ID: strings.Repeat("a", 64), Image: permit.Target.RuntimeImage, Project: project, Readonly: true, Memory: 384 << 20, NanoCPUs: 750000000, PIDs: 64, Restart: "no", Mounts: []ContainerMount{
				{Source: filepath.Join(root, "releases", permit.BundleSHA256, "source"), Destination: "/app", Type: "bind"},
				{Name: project + "_state", Destination: "/state", Type: "volume", RW: true},
			}}}
			switch scenario {
			case "foreign-project":
				runner.evidence.Project = "kodex_foreign"
			case "foreign-volume":
				runner.evidence.Mounts[1].Name = "foreign_state"
			case "writable-source":
				runner.evidence.Mounts[0].RW = true
			case "foreign-image":
				runner.evidence.Image = "foreign:latest"
			case "resources":
				runner.evidence.Memory = 0
			}
			err = (Helper{Target: permit.Target, RootDirectory: root, Runner: runner}).Boot(context.Background())
			if scenario == "committed" {
				if err != nil || len(runner.arguments) != 4 || runner.arguments[3] != runner.evidence.ID {
					t.Fatal("committed container not started exactly")
				}
			} else if len(runner.arguments) != 0 {
				t.Fatal("uncommitted or substituted container started")
			}
			if state == "SUCCEEDED" && scenario != "committed" && err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}
