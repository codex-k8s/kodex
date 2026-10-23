package session

import "testing"

// TestAssumeQueryUsesOnlyRegisteredStatements checks the five exact capability
// statements and rejects empty, unknown and SQL-shaped role inputs. It tests
// selection only, not SET ROLE execution, LOGIN grants or identity readback.
// TestAssumeQueryUsesOnlyRegisteredStatements проверяет пять точных capability
// statements и отклоняет пустое, неизвестное и SQL-подобное имя роли. Проверяется
// только выбор, не исполнение SET ROLE, права LOGIN и readback идентичности.
func TestAssumeQueryUsesOnlyRegisteredStatements(t *testing.T) {
	t.Parallel()
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("load session queries: %v", err)
	}
	for _, testCase := range []struct {
		capability string
		want       string
	}{
		{CapabilityIssuer, queries.issuerAssume},
		{CapabilityVerifier, queries.verifierAssume},
		{CapabilityPublisher, queries.publisherAssume},
		{CapabilityReadbackAttestor, queries.readbackAttestorAssume},
		{CapabilityRestoreController, queries.restoreControllerAssume},
		{"", ""},
		{"unregistered", ""},
		{"SET ROLE superuser;", ""},
	} {
		t.Run(testCase.capability, func(t *testing.T) {
			got, err := assumeQuery(queries, testCase.capability)
			if testCase.want == "" {
				if err == nil || got != "" {
					t.Fatal("unregistered capability selected SQL")
				}
				return
			}
			if err != nil || got != testCase.want {
				t.Fatalf("registered capability query changed: %v", err)
			}
		})
	}
}
