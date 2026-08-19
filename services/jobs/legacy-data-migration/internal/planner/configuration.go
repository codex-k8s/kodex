package planner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

// ConfigurationProjection — один project-scoped конфигурационный граф,
// выведенный из единого repeatable-read source snapshot.
type ConfigurationProjection struct {
	LegacyProjectID int64
	ProjectName     string
	Rows            []model.SnapshotRow
	SourceSHA256    string
	Counts          map[string]uint64
	TableSHA256     map[string]string
}

// ConfigurationProjects отделяет активную конфигурацию проектов от runtime
// истории. Связанные строки выбираются только по явным foreign-key значениям;
// глобальные профили без project ownership в новый граф не копируются.
func ConfigurationProjects(projection []model.SnapshotRow) ([]ConfigurationProjection, error) {
	allCounts := emptyCounts()
	for _, entry := range projection {
		if !inventory.Contains(entry.Table) {
			return nil, errors.New("configuration source contains an unknown table")
		}
		if len(entry.Payload) != 0 {
			allCounts[entry.Table]++
		}
	}
	rows, _, err := decodeSource(projection, allCounts)
	if err != nil {
		return nil, err
	}
	projects := append([]sourceRow(nil), rows["matter_codex_projects"]...)
	sort.Slice(projects, func(i, j int) bool { return number(projects[i], "id") < number(projects[j], "id") })
	if len(projects) == 0 {
		return nil, errors.New("configuration source has no projects")
	}
	result := make([]ConfigurationProjection, 0, len(projects))
	for _, project := range projects {
		selected, selectErr := selectConfigurationProject(rows, number(project, "id"))
		if selectErr != nil {
			return nil, selectErr
		}
		encoded, counts, tableSHA256, sourceSHA256, encodeErr := encodeConfigurationProjection(selected)
		if encodeErr != nil {
			return nil, encodeErr
		}
		result = append(result, ConfigurationProjection{
			LegacyProjectID: number(project, "id"), ProjectName: text(project, "name"), Rows: encoded,
			SourceSHA256: sourceSHA256, Counts: counts, TableSHA256: tableSHA256,
		})
	}
	return result, nil
}

func selectConfigurationProject(rows map[string][]sourceRow, projectID int64) (map[string][]sourceRow, error) {
	if projectID == 0 {
		return nil, errors.New("configuration project identifier is invalid")
	}
	selected := make(map[string][]sourceRow)
	project := selectRows(rows["matter_codex_projects"], func(row sourceRow) bool { return number(row, "id") == projectID })
	if len(project) != 1 {
		return nil, errors.New("configuration project boundary is ambiguous")
	}
	selected["matter_codex_projects"] = project

	selected["matter_codex_chats"] = selectRows(rows["matter_codex_chats"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && text(row, "status") == "active"
	})
	selected["matter_codex_agent_roles"] = selectRows(rows["matter_codex_agent_roles"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && boolean(row, "enabled")
	})
	chatIDs, roleIDs := idSet(selected["matter_codex_chats"], "id"), idSet(selected["matter_codex_agent_roles"], "id")
	if len(chatIDs) == 0 || len(roleIDs) == 0 {
		return nil, fmt.Errorf("project %d has no active chats or roles", projectID)
	}
	selected["matter_codex_chat_participants"] = selectRows(rows["matter_codex_chat_participants"], func(row sourceRow) bool {
		return boolean(row, "enabled") && chatIDs[number(row, "chat_id")] && roleIDs[number(row, "role_id")]
	})

	selected["matter_codex_project_repositories"] = selectRows(rows["matter_codex_project_repositories"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID
	})
	repositoryIDs := idSet(selected["matter_codex_project_repositories"], "repository_id")
	selected["matter_codex_repositories"] = selectRows(rows["matter_codex_repositories"], func(row sourceRow) bool {
		return repositoryIDs[number(row, "id")] && text(row, "status") == "active"
	})
	selectedRepositoryIDs := idSet(selected["matter_codex_repositories"], "id")
	selected["matter_codex_project_repositories"] = selectRows(selected["matter_codex_project_repositories"], func(row sourceRow) bool {
		return selectedRepositoryIDs[number(row, "repository_id")]
	})
	selected["matter_codex_chat_repositories"] = selectRows(rows["matter_codex_chat_repositories"], func(row sourceRow) bool {
		return chatIDs[number(row, "chat_id")] && selectedRepositoryIDs[number(row, "repository_id")]
	})

	selected["matter_codex_project_runtime_variables"] = selectRows(rows["matter_codex_project_runtime_variables"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && boolean(row, "enabled")
	})
	variableIDs := idSet(selected["matter_codex_project_runtime_variables"], "id")
	selected["matter_codex_agent_role_runtime_variables"] = selectRows(rows["matter_codex_agent_role_runtime_variables"], func(row sourceRow) bool {
		return roleIDs[number(row, "role_id")] && variableIDs[number(row, "variable_id")]
	})

	selected["matter_codex_policy_revisions"] = selectRows(rows["matter_codex_policy_revisions"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && text(row, "status") == "active"
	})
	policyIDs := idSet(selected["matter_codex_policy_revisions"], "id")
	selected["matter_codex_role_capabilities"] = selectRows(rows["matter_codex_role_capabilities"], func(row sourceRow) bool {
		return policyIDs[number(row, "policy_revision_id")] && roleIDs[number(row, "role_id")] && boolean(row, "enabled")
	})
	selected["matter_codex_role_relationship_policies"] = selectRows(rows["matter_codex_role_relationship_policies"], func(row sourceRow) bool {
		return policyIDs[number(row, "policy_revision_id")] && roleIDs[number(row, "source_role_id")] &&
			roleIDs[number(row, "target_role_id")] && boolean(row, "enabled")
	})

	selected["matter_codex_mattermost_bot_identities"] = selectRows(rows["matter_codex_mattermost_bot_identities"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && roleIDs[number(row, "role_id")]
	})
	selected["matter_codex_automation_schedules"] = selectRows(rows["matter_codex_automation_schedules"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && roleIDs[number(row, "target_agent_role_id")] && chatIDs[number(row, "target_chat_id")]
	})
	selectClusterConfiguration(selected, rows, projectID, roleIDs, chatIDs, variableIDs)
	selectConfigurationCredentials(selected, rows, project[0])
	return selected, nil
}

func selectClusterConfiguration(selected, rows map[string][]sourceRow, projectID int64,
	roleIDs, chatIDs, variableIDs map[int64]bool,
) {
	selected["matter_codex_cluster_admin_subjects"] = selectRows(rows["matter_codex_cluster_admin_subjects"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID
	})
	selected["matter_codex_cluster_admin_bot_bindings"] = selectRows(rows["matter_codex_cluster_admin_bot_bindings"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && roleIDs[number(row, "role_id")]
	})
	selected["matter_codex_cluster_admin_session_bindings"] = selectRows(rows["matter_codex_cluster_admin_session_bindings"], func(row sourceRow) bool {
		return number(row, "project_id") == projectID && roleIDs[number(row, "role_id")] && chatIDs[number(row, "chat_id")]
	})
	selected["matter_codex_cluster_admin_runtime_variable_bindings"] = selectRows(rows["matter_codex_cluster_admin_runtime_variable_bindings"], func(row sourceRow) bool {
		return roleIDs[number(row, "role_id")] && variableIDs[number(row, "variable_id")]
	})
	selected["matter_codex_cluster_admin_dependencies"] = selectRows(rows["matter_codex_cluster_admin_dependencies"], func(row sourceRow) bool {
		return roleIDs[number(row, "role_id")]
	})
}

func selectConfigurationCredentials(selected, rows map[string][]sourceRow, project sourceRow) {
	openAI, github := make(map[string]bool), make(map[string]bool)
	if name := text(project, "github_account_name"); name != "" {
		github[name] = true
	}
	for _, row := range selected["matter_codex_agent_roles"] {
		if name := text(row, "openai_account_name"); name != "" {
			openAI[name] = true
		}
		if name := text(row, "github_account_name"); name != "" {
			github[name] = true
		}
	}
	for _, row := range selected["matter_codex_repositories"] {
		if name := text(row, "github_account_name"); name != "" {
			github[name] = true
		}
	}
	selected["matter_codex_openai_accounts"] = selectRows(rows["matter_codex_openai_accounts"], func(row sourceRow) bool {
		return openAI[text(row, "name")] && text(row, "status") == "authorized"
	})
	selected["matter_codex_github_accounts"] = selectRows(rows["matter_codex_github_accounts"], func(row sourceRow) bool {
		return github[text(row, "name")] && text(row, "status") == "configured"
	})
	credentialIDs := idSet(selected["matter_codex_openai_accounts"], "credential_id")
	for id := range idSet(selected["matter_codex_github_accounts"], "credential_id") {
		credentialIDs[id] = true
	}
	selected["matter_codex_credentials"] = selectRows(rows["matter_codex_credentials"], func(row sourceRow) bool {
		return credentialIDs[number(row, "id")]
	})
}

func encodeConfigurationProjection(selected map[string][]sourceRow) ([]model.SnapshotRow, map[string]uint64, map[string]string, string, error) {
	result := make([]model.SnapshotRow, 0, 256)
	counts, tableSHA256 := emptyCounts(), make(map[string]string, len(inventory.Tables))
	snapshotHash := sha256.New()
	for _, table := range inventory.Tables {
		rows := append([]sourceRow(nil), selected[table]...)
		payloads := make([][]byte, 0, len(rows))
		for _, row := range rows {
			encoded, err := json.Marshal(row)
			if err != nil {
				return nil, nil, nil, "", errors.New("encode configuration projection row")
			}
			payloads = append(payloads, encoded)
		}
		sort.Slice(payloads, func(i, j int) bool { return bytes.Compare(payloads[i], payloads[j]) < 0 })
		tableHash := sha256.New()
		writeFramed(tableHash, []byte(table))
		for _, payload := range payloads {
			writeFramed(tableHash, payload)
			result = append(result, model.SnapshotRow{Table: table, Payload: payload})
			counts[table]++
		}
		tableSHA256[table] = hex.EncodeToString(tableHash.Sum(nil))
		writeFramed(snapshotHash, []byte(table))
		writeFramed(snapshotHash, []byte(tableSHA256[table]))
	}
	return result, counts, tableSHA256, hex.EncodeToString(snapshotHash.Sum(nil)), nil
}

func emptyCounts() map[string]uint64 {
	result := make(map[string]uint64, len(inventory.Tables))
	for _, table := range inventory.Tables {
		result[table] = 0
	}
	return result
}

func selectRows(rows []sourceRow, predicate func(sourceRow) bool) []sourceRow {
	result := make([]sourceRow, 0, len(rows))
	for _, row := range rows {
		if predicate(row) {
			result = append(result, row)
		}
	}
	return result
}

func idSet(rows []sourceRow, key string) map[int64]bool {
	result := make(map[int64]bool, len(rows))
	for _, row := range rows {
		if id := number(row, key); id != 0 {
			result[id] = true
		}
	}
	return result
}
