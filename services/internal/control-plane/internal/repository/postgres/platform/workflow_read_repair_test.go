package platform

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// workflowRepairRow provides positional values for a stored workflow row; callers must supply
// matching destination types for reflective assignment.
// workflowRepairRow предоставляет позиционные значения сохранённой строки workflow; вызывающий
// должен передать совпадающие типы назначений для присваивания через reflection.
type workflowRepairRow []any

// Scan copies fixture values into typed destinations without database access or production
// validation.
// Scan копирует тестовые значения в типизированные назначения без доступа к БД либо
// производственной валидации.
func (row workflowRepairRow) Scan(dest ...any) error {
	for i, value := range row {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

// TestWorkflowUnexecutableConfigurationCanBeReadForRepair keeps structurally readable but
// unexecutable draft/published workflow configuration available for repair and assistant
// inspection, while rejecting corrupt stored JSON.
// TestWorkflowUnexecutableConfigurationCanBeReadForRepair сохраняет структурно читаемую, но
// неисполнимую черновую и опубликованную конфигурацию workflow доступной для ремонта и просмотра
// ассистентом, отклоняя повреждённый сохранённый JSON.
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
