package integrationpackage

// CredentialReadProbes returns only the published health capability and registered inputless
// READ/NONE operations. It never infers object reads or writes from identity/project metadata.
// CredentialReadProbes возвращает только опубликованный health и зарегистрированные READ/NONE
// операции без обязательных inputs. Identity/project metadata не подтверждают чтение объектов/запись.
func CredentialReadProbes(definition Package) []Capability {
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" {
		return nil
	}
	result := []Capability{health}
	seen := map[string]bool{health.Operation: true}
	for _, capability := range definition.Spec.Capabilities {
		if seen[capability.Operation] || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" {
			continue
		}
		registered := false
		switch definition.Spec.Adapter {
		case "SENTRY":
			registered = capability.Operation == "sentry.issue.list"
		case "GITLAB":
			switch capability.Operation {
			case "gitlab.project.list", "gitlab.issue.list", "gitlab.branch.list", "gitlab.commit.list", "gitlab.repository.tree.list", "gitlab.merge_request.list", "gitlab.pipeline.list", "gitlab.job.list":
				registered = true
			}
		case "JIRA":
			registered = definition.Metadata.Key == "jira-server-dc" && capability.Operation == "jira.identity.read"
		case "CONFLUENCE":
			registered = definition.Metadata.Key == "confluence-server-dc" && capability.Operation == "confluence.identity.read"
		}
		for _, field := range capability.InputFields {
			if field.Required {
				registered = false
			}
		}
		if registered {
			result = append(result, capability)
			seen[capability.Operation] = true
		}
	}
	return result
}
