package platform

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type workflowRepairRow []any

func (row workflowRepairRow) Scan(dest ...any) error {
	for i, value := range row {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

func TestWorkflowUnexecutableConfigurationCanBeReadForRepair(t *testing.T) {
	v := validWorkflowFixture()
	for i := range v.Steps {
		v.Steps[i].AgentRef = v.CoordinatorAgentRef
		v.Steps[i].RequiredCapabilityKeys = []string{"platform.run.delegate"}
	}
	if validWorkflowVersion(v) {
		t.Fatal("unexecutable fixture accepted for execution")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	row := workflowRepairRow{"wfl_repair_fixture", "prj_fixture", v.Name, v.Purpose, v.CoordinatorAgentRef, "PUBLISHED", int64(3), raw, raw, int32(1), time.Time{}, time.Time{}, "wfv_published_fixture"}
	read, err := scanWorkflow(row, false)
	if err != nil || read.Draft == nil || read.Published == nil {
		t.Fatalf("unexecutable configuration unavailable for repair: %v", err)
	}
	if _, err := assistantWorkflowMetadataJSON(read); err != nil {
		t.Fatalf("assistant cannot inspect configuration for repair: %v", err)
	}
	row[7] = []byte("{")
	if _, err := scanWorkflow(row, false); err == nil {
		t.Fatal("corrupt stored configuration accepted")
	}
}
