package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// jiraRecoveryProject carries only the addressed stable project ID/key, never project rights.
// jiraRecoveryProject содержит только addressed stable project ID/key, а не права проекта.
type jiraRecoveryProject struct{ ID, Key string }

// jiraRecoveryIssue adds immutable creator/type metadata to the exact create payload observation.
// These external coordinates never establish the platform root initiator or choose a credential.
// jiraRecoveryIssue добавляет immutable creator/type metadata к exact create payload observation.
// External coordinates не назначают platform root initiator и не выбирают credential.
type jiraRecoveryIssue struct {
	ID, Key string
	Custom  map[string]json.RawMessage `json:"-"`
	Fields  struct {
		Summary     string
		Description *string
		Labels      []string
		Project     jiraRecoveryProject
		Creator     atlassianRecoveryUser
		IssueType   struct{ Name string }
		Reporter    struct{ Key, Name string }
	}
}

// UnmarshalJSON retains requested custom values beside the typed projection. Its caller first
// rejects ambiguous JSON; no unknown provider field can select a URL, credential or write target.
// UnmarshalJSON сохраняет запрошенные custom values рядом с типизированной проекцией. Caller
// предварительно отклоняет неоднозначный JSON; поля провайдера не выбирают URL, credential или цель.
func (issue *jiraRecoveryIssue) UnmarshalJSON(raw []byte) error {
	type plain jiraRecoveryIssue
	var value plain
	var root struct{ Fields map[string]json.RawMessage }
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return err
	}
	value.Custom = root.Fields
	*issue = jiraRecoveryIssue(value)
	return nil
}

// jiraRecoveryComment keeps only fields required to attribute an exact marked comment.
// jiraRecoveryComment содержит только поля для attribution exact marked comment.
type jiraRecoveryComment struct {
	ID, Body             string
	Author, UpdateAuthor atlassianRecoveryUser
}

// jiraRecoveryPage represents explicit Server offsets; absent/changed totals stay unconfirmed.
// jiraRecoveryPage описывает явные Server offsets; absent/changed totals остаются unconfirmed.
type jiraRecoveryPage struct {
	StartAt, MaxResults, Total *int
	Issues                     []jiraRecoveryIssue
	Comments                   []jiraRecoveryComment
}

// recoverJiraServerEffect uses only current identity, configured project and bounded fixed reads.
// Empty, duplicate or changed evidence remains unknown and never reaches a create/comment POST.
// recoverJiraServerEffect использует только current identity, configured project и bounded fixed reads.
// Empty, duplicate либо changed evidence остаётся unknown и не приводит к create/comment POST.
func (adapter *Adapter) recoverJiraServerEffect(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte, userKey string) (Result, error) {
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/project/"+url.PathEscape(configuration["project_key"]), nil, nil, "")
	if err != nil {
		return Result{}, err
	}
	var project jiraRecoveryProject
	if decodeAtlassianRecoveryJSON(body, &project) != nil || !decimalID(project.ID) || project.Key != configuration["project_key"] {
		return Result{}, &UnknownOutcomeError{}
	}
	if request.Operation == "jira.issue.create" {
		return adapter.recoverJiraServerCreate(ctx, request, capability, configuration, input, userKey, project)
	}
	return adapter.recoverJiraServerComment(ctx, request, capability, configuration, input, userKey, project)
}

// readJiraRecoveryIssue addresses only a project-local key, requests required metadata explicitly
// and validates project ID as well as key. Provider links and supplied self URLs are never followed.
// readJiraRecoveryIssue читает только project-local key, явно запрашивает нужные metadata
// и сверяет project ID вместе с key. Provider links и supplied self URLs не используются.
func (adapter *Adapter) readJiraRecoveryIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, key string, project jiraRecoveryProject) (jiraRecoveryIssue, error) {
	if !jiraIssueInProject(key, project.Key) {
		return jiraRecoveryIssue{}, &UnknownOutcomeError{}
	}
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/issue/"+url.PathEscape(key), url.Values{"fields": {jiraCreateReadFields(configuration)}}, nil, "")
	if err != nil {
		return jiraRecoveryIssue{}, err
	}
	var issue jiraRecoveryIssue
	if decodeAtlassianRecoveryJSON(body, &issue) != nil || !decimalID(issue.ID) || issue.Key != key || issue.Fields.Project != project {
		return jiraRecoveryIssue{}, &UnknownOutcomeError{}
	}
	return issue, nil
}

// jiraRecoveryPageNext requires contiguous bounded Server pages and a stable observed total.
// Its terminal flag concerns only this returned collection, never global permissions or absence.
// jiraRecoveryPageNext требует contiguous bounded Server pages и stable observed total.
// Terminal flag относится только к возвращённой collection, не к global permissions либо absence.
func jiraRecoveryPageNext(page jiraRecoveryPage, offset, count int, total *int) (int, bool, error) {
	if page.StartAt == nil || *page.StartAt != offset || page.MaxResults == nil || *page.MaxResults < 1 || *page.MaxResults > 50 || count > *page.MaxResults || page.Total == nil || *page.Total < 0 || *page.Total > 250 || total != nil && *total != *page.Total {
		return 0, false, &UnknownOutcomeError{}
	}
	next, err := atlassianServerNext(offset, count, 50, *page.Total)
	if err != nil {
		return 0, false, &UnknownOutcomeError{}
	}
	if next == "" {
		return offset + count, true, nil
	}
	number, err := strconv.Atoi(next)
	if err != nil {
		return 0, false, &UnknownOutcomeError{}
	}
	return number, false, nil
}

// jiraRecoveryCreateMatches requires the authenticated creator, exact project/type/payload and
// one owner marker. Reporter/display name and a search match alone are not provenance.
// jiraRecoveryCreateMatches требует authenticated creator, exact project/type/payload и один
// owner marker. Reporter/display name либо search match сами не являются provenance.
func jiraRecoveryCreateMatches(issue jiraRecoveryIssue, project jiraRecoveryProject, userKey, label, summary, description, issueType string) bool {
	actual := ""
	if issue.Fields.Description != nil {
		actual = *issue.Fields.Description
	}
	count := 0
	for _, value := range issue.Fields.Labels {
		if value == label {
			count++
		}
	}
	return decimalID(issue.ID) && jiraIssueInProject(issue.Key, project.Key) && issue.Fields.Project == project && issue.Fields.Creator.Key == userKey && validAtlassianRecoveryKey(userKey) && issue.Fields.IssueType.Name == issueType && issue.Fields.Summary == summary && actual == description && count == 1
}

// recoverJiraServerCreate exhausts a finite exact-label search, then checks the same issue ID
// through an addressed read. It never adopts partial search output or creates a replacement issue.
// recoverJiraServerCreate исчерпывает finite exact-label search и сверяет тот же issue ID
// адресным чтением. Partial search output не принимается, replacement issue не создаётся.
func (adapter *Adapter) recoverJiraServerCreate(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte, userKey string, project jiraRecoveryProject) (Result, error) {
	var value jiraCreateIntent
	if decodeProviderJSON(input, &value) != nil {
		return Result{}, &UnknownOutcomeError{}
	}
	label := "kodex-effect-" + request.EffectKey
	escaped, valid := jiraServerLiteral(label)
	if !valid || len(label) > 255 || strings.ContainsRune(label, ' ') {
		return Result{}, &UnknownOutcomeError{}
	}
	query := fmt.Sprintf(`project = "%s" AND labels = "%s" ORDER BY key ASC`, project.Key, escaped)
	offset := 0
	var total *int
	var candidate *jiraRecoveryIssue
	seen := map[string]bool{}
	exhausted := false
	for iteration := 0; iteration < 5; iteration++ {
		body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/search", url.Values{"jql": {query}, "fields": {jiraCreateReadFields(configuration)}, "startAt": {strconv.Itoa(offset)}, "maxResults": {"50"}}, nil, "")
		if err != nil {
			return Result{}, err
		}
		var page jiraRecoveryPage
		if decodeAtlassianRecoveryJSON(body, &page) != nil {
			return Result{}, &UnknownOutcomeError{}
		}
		next, done, err := jiraRecoveryPageNext(page, offset, len(page.Issues), total)
		if err != nil {
			return Result{}, err
		}
		total = page.Total
		for _, issue := range page.Issues {
			if seen[issue.ID] || candidate != nil || !jiraRecoveryCreateMatches(issue, project, userKey, label, value.Summary, value.Description, configuration["issue_type"]) || !jiraRecoveryCustomMatches(issue, configuration, value, userKey) {
				return Result{}, &UnknownOutcomeError{}
			}
			seen[issue.ID] = true
			copy := issue
			candidate = &copy
		}
		if done {
			exhausted = true
			break
		}
		offset = next
	}
	if !exhausted || candidate == nil {
		return Result{}, &UnknownOutcomeError{}
	}
	issue, err := adapter.readJiraRecoveryIssue(ctx, request, capability, configuration, candidate.Key, project)
	if err != nil {
		return Result{}, err
	}
	if issue.ID != candidate.ID || !jiraRecoveryCreateMatches(issue, project, userKey, label, value.Summary, value.Description, configuration["issue_type"]) || !jiraRecoveryCustomMatches(issue, configuration, value, userKey) {
		return Result{}, &UnknownOutcomeError{}
	}
	return providerResult(request, "jira-issue:"+issue.Key, map[string]any{"id": issue.ID, "key": issue.Key})
}

// jiraRecoveryCustomMatches proves each explicitly approved scalar/option and self reporter from
// immutable input/configuration. Missing or changed evidence stays UNKNOWN; metadata is not reread
// and no create is retried after an uncertain external effect.
// jiraRecoveryCustomMatches подтверждает каждое явно одобренное значение и self reporter по
// неизменяемым input/configuration. Неточное evidence остаётся UNKNOWN; create не повторяется.
func jiraRecoveryCustomMatches(issue jiraRecoveryIssue, configuration map[string]string, intent jiraCreateIntent, userKey string) bool {
	expected, allowed, err := jiraCreateFieldValues(configuration, intent)
	if err != nil || intent.ReporterSelf && issue.Fields.Reporter.Key != userKey {
		return false
	}
	for id, value := range expected {
		raw, exists := issue.Custom[id]
		if !exists || string(raw) == "null" {
			return false
		}
		switch allowed[id].Type {
		case "OPTION":
			var option struct{ ID string }
			if json.Unmarshal(raw, &option) != nil || option.ID != value.(map[string]string)["id"] {
				return false
			}
		case "NUMBER":
			var number json.Number
			if json.Unmarshal(raw, &number) != nil || len(number.String()) > 128 || len(raw) == 0 || raw[0] == '"' {
				return false
			}
			if index := strings.IndexAny(number.String(), "eE"); index >= 0 {
				exponent, err := strconv.Atoi(number.String()[index+1:])
				if err != nil || exponent < -64 || exponent > 64 {
					return false
				}
			}
			actual, ok := new(big.Rat).SetString(number.String())
			wanted, valid := new(big.Rat).SetString(value.(json.Number).String())
			if !ok || !valid || actual.Cmp(wanted) != 0 {
				return false
			}
		case "BOOLEAN":
			var actual bool
			if json.Unmarshal(raw, &actual) != nil || actual != value.(bool) {
				return false
			}
		default:
			var actual string
			if json.Unmarshal(raw, &actual) != nil || actual != value.(string) {
				return false
			}
		}
	}
	return true
}

// jiraRecoveryCommentMatches attributes exactly one canonical body marker to the current key;
// a changed update author is left unknown rather than adopted as this intent's comment.
// jiraRecoveryCommentMatches связывает один canonical body marker с current key;
// changed update author остаётся unknown, а не принимается за comment данного intent.
func jiraRecoveryCommentMatches(comment jiraRecoveryComment, userKey, body, marker string) bool {
	return decimalID(comment.ID) && comment.Author.Key == userKey && comment.UpdateAuthor.Key == userKey && comment.Body == body && strings.Count(comment.Body, marker) == 1
}

// recoverJiraServerComment exhausts bounded comments, verifies a unique exact body/author and
// reads that ID under the same issue before rechecking the issue's current configured project.
// recoverJiraServerComment исчерпывает bounded comments, проверяет unique exact body/author и
// читает этот ID в той же issue, затем перепроверяет current configured project задачи.
func (adapter *Adapter) recoverJiraServerComment(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte, userKey string, project jiraRecoveryProject) (Result, error) {
	var value struct {
		IssueKey string `json:"issue_key"`
		Body     string
	}
	if decodeProviderJSON(input, &value) != nil {
		return Result{}, &UnknownOutcomeError{}
	}
	issue, err := adapter.readJiraRecoveryIssue(ctx, request, capability, configuration, value.IssueKey, project)
	if err != nil {
		return Result{}, err
	}
	marker := "[kodex-effect:" + request.EffectKey + "]"
	expected := value.Body + "\n\n" + marker
	path := "/issue/" + url.PathEscape(value.IssueKey) + "/comment"
	offset := 0
	var total *int
	var candidate *jiraRecoveryComment
	seen := map[string]bool{}
	exhausted := false
	for iteration := 0; iteration < 5; iteration++ {
		raw, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, path, url.Values{"startAt": {strconv.Itoa(offset)}, "maxResults": {"50"}, "orderBy": {"created"}}, nil, "")
		if err != nil {
			return Result{}, err
		}
		var page jiraRecoveryPage
		if decodeAtlassianRecoveryJSON(raw, &page) != nil {
			return Result{}, &UnknownOutcomeError{}
		}
		next, done, err := jiraRecoveryPageNext(page, offset, len(page.Comments), total)
		if err != nil {
			return Result{}, err
		}
		total = page.Total
		for _, comment := range page.Comments {
			if !decimalID(comment.ID) || seen[comment.ID] {
				return Result{}, &UnknownOutcomeError{}
			}
			seen[comment.ID] = true
			if !strings.Contains(comment.Body, marker) {
				continue
			}
			if candidate != nil || !jiraRecoveryCommentMatches(comment, userKey, expected, marker) {
				return Result{}, &UnknownOutcomeError{}
			}
			copy := comment
			candidate = &copy
		}
		if done {
			exhausted = true
			break
		}
		offset = next
	}
	if !exhausted || candidate == nil {
		return Result{}, &UnknownOutcomeError{}
	}
	raw, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, path+"/"+candidate.ID, nil, nil, "")
	if err != nil {
		return Result{}, err
	}
	var comment jiraRecoveryComment
	if decodeAtlassianRecoveryJSON(raw, &comment) != nil || comment.ID != candidate.ID || !jiraRecoveryCommentMatches(comment, userKey, expected, marker) {
		return Result{}, &UnknownOutcomeError{}
	}
	current, err := adapter.readJiraRecoveryIssue(ctx, request, capability, configuration, value.IssueKey, project)
	if err != nil {
		return Result{}, err
	}
	if current.ID != issue.ID {
		return Result{}, &UnknownOutcomeError{}
	}
	return providerResult(request, "jira-comment:"+comment.ID, map[string]any{"comment_id": comment.ID, "issue_key": value.IssueKey})
}
