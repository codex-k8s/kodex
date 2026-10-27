package platform

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestSearchRankingPreservesOrderAndPayload checks literal ordering goldens for relevance priority,
// descending nanosecond time, equal instants in different time zones and kind/ref tie-breakers.
// Equal keys retain all entries without a stability promise; nil/allocated-empty shape and every
// candidate field are preserved. The fixture performs no permission or pagination checks.
// TestSearchRankingPreservesOrderAndPayload проверяет буквальный ожидаемый порядок по приоритету
// релевантности, убыванию времени с наносекундами, равным моментам в разных часовых поясах и kind/ref.
// Равные ключи сохраняют все элементы без обещания стабильности; nil, выделенный пустой срез и поля
// кандидатов сохраняются. Оснастка не проверяет полномочия и пагинацию.
func TestSearchRankingPreservesOrderAndPayload(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	for _, test := range []struct {
		name      string
		items     []rankedSearchResult
		want      []string
		unordered bool
	}{
		{"nil", nil, []string{}, false},
		{"empty", []rankedSearchResult{}, []string{}, false},
		{"all ordering levels", []rankedSearchResult{
			{entity.SearchResult{Kind: "PROJECT", Ref: "a", Title: "last relevance"}, 2, now.Add(time.Hour)},
			{entity.SearchResult{Kind: "WORKFLOW", Ref: "z", Title: "first relevance"}, 0, time.Time{}},
			{entity.SearchResult{Kind: "WORKFLOW", Ref: "a", Title: "newest"}, 1, now.Add(time.Nanosecond)},
			{entity.SearchResult{Kind: "WORKFLOW", Ref: "a", Title: "workflow"}, 1, now},
			{entity.SearchResult{Kind: "PROJECT", Ref: "z", Title: "project z"}, 1, now.In(time.FixedZone("fixture", 3*3600))},
			{entity.SearchResult{Kind: "PROJECT", Ref: "a", Title: "project a", Subtitle: "payload", ProjectRef: "project-ref"}, 1, now},
			{entity.SearchResult{Kind: "PROJECT", Ref: "a", Title: "older"}, 1, now.Add(-time.Nanosecond)},
		}, []string{"first relevance", "newest", "project a", "project z", "workflow", "older", "last relevance"}, false},
		{"equal keys", []rankedSearchResult{
			{entity.SearchResult{Kind: "PROJECT", Ref: "a", Title: "second payload"}, 1, now},
			{entity.SearchResult{Kind: "PROJECT", Ref: "a", Title: "first payload"}, 1, now},
		}, []string{"first payload", "second payload"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wasNil := test.items == nil
			original := make(map[string]rankedSearchResult, len(test.items))
			for _, item := range test.items {
				original[item.Title] = item
			}
			sortSearchResults(test.items)
			titles := []string{}
			for _, item := range test.items {
				if !reflect.DeepEqual(item, original[item.Title]) {
					t.Fatal("search ranking changed a candidate payload")
				}
				titles = append(titles, item.Title)
			}
			if test.unordered {
				sort.Strings(titles)
			}
			if (test.items == nil) != wasNil || !reflect.DeepEqual(titles, test.want) {
				t.Fatal("search ranking changed priority, count or nil shape")
			}
		})
	}
}
