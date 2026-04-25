package staffrun

import (
	"strings"
	"testing"
)

func TestRunListQueriesIncludeDiscussionAndReviewerTriggerLabels(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"list_all":      queryListAll,
		"list_for_user": queryListForUser,
	}

	for name, query := range queries {
		if !strings.Contains(query, "ILIKE 'run:%'") {
			t.Fatalf("%s query must keep run trigger filter", name)
		}
		if !strings.Contains(query, "= 'mode:discussion'") {
			t.Fatalf("%s query must include discussion trigger filter", name)
		}
		if !strings.Contains(query, "ILIKE 'need:reviewer'") {
			t.Fatalf("%s query must include reviewer trigger filter", name)
		}
	}
}

func TestRunQueriesNormalizeDiscussionTriggerKind(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"list_all":            queryListAll,
		"list_for_user":       queryListForUser,
		"list_jobs_all":       queryListJobsAll,
		"list_jobs_for_user":  queryListJobsForUser,
		"list_waits_all":      queryListWaitsAll,
		"list_waits_for_user": queryListWaitsForUser,
		"get_by_id":           queryGetByID,
	}

	for name, query := range queries {
		if !strings.Contains(query, "discussion_mode") {
			t.Fatalf("%s query must inspect discussion_mode payload flag", name)
		}
		if !strings.Contains(query, "THEN 'discussion'") {
			t.Fatalf("%s query must normalize trigger_kind to discussion", name)
		}
	}
}

func TestRunQueriesSupportRealtimeListPagination(t *testing.T) {
	t.Parallel()

	if !strings.Contains(queryListAll, "OFFSET $2") {
		t.Fatal("list_all query must support pagination offset")
	}
	if !strings.Contains(queryListForUser, "OFFSET $3") {
		t.Fatal("list_for_user query must support pagination offset")
	}
	if !strings.Contains(queryCountAll, "COUNT(*)") {
		t.Fatal("count_all query must count visible runs")
	}
	if !strings.Contains(queryCountForUser, "COUNT(*)") {
		t.Fatal("count_for_user query must count visible runs")
	}
}
