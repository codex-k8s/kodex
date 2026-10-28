package workflow

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// MaximumQAReportBytes bounds untrusted structured report data; captured logs remain separately
// immutable owner-pinned files and the report is never an execution attestation.
// MaximumQAReportBytes ограничивает недоверенный report; captured logs остаются отдельными
// immutable owner-pinned файлами, а report никогда не считается execution attestation.
const MaximumQAReportBytes = 32 << 10

// QAReport names reported tests, immutable captured log selectors and optional real findings.
// The owner must resolve every selector and match repository/head against accepted dependencies.
// QAReport описывает заявленные тесты, immutable selectors captured logs и необязательные findings.
// Owner разрешает каждый selector и сверяет repository/head с принятыми зависимостями.
type QAReport struct {
	Schema     string      `json:"schema"`
	Repository string      `json:"repository"`
	Number     int64       `json:"pullRequestNumber"`
	HeadSHA    string      `json:"headSha"`
	Verdict    string      `json:"verdict"`
	Tests      []QATest    `json:"tests"`
	Findings   []QAFinding `json:"findings"`
	Issue      *QAIssue    `json:"issue,omitempty"`
}

// QATest records employee-reported command/outcome and the exact already captured output file.
// No command string is executed by the owner or treated as independent proof that it ran.
// QATest хранит заявленные сотрудником command/outcome и exact captured output file.
// Owner не исполняет строку command и не считает её независимым доказательством запуска.
type QATest struct {
	Name     string                         `json:"name"`
	Command  string                         `json:"command"`
	ExitCode int32                          `json:"exitCode"`
	Log      *entity.WorkflowArtifactSource `json:"log"`
}

// QAFinding is bounded review data and never opens an issue without the separate owner slot.
// QAFinding — bounded review data; она не создаёт Issue без отдельного owner slot.
type QAFinding struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
}

// QAIssue contains the single aggregate finding publication the employee requests, not a target.
// The target repository comes from the accepted subject and the current grant, never this report.
// QAIssue содержит одну запрошенную публикацию совокупных findings, не её цель. Repository
// определяется принятым subject и current grant, а не report.
type QAIssue struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// DecodeQAReport rejects unknown, duplicate, case-folded or nullable fields before trusting shape.
// It proves only report structure; the caller still owns capture, ACL, CLEAN and exact-head checks.
// DecodeQAReport отвергает unknown, duplicate, case-folded и null поля до разбора формы.
// Функция проверяет только структуру; capture, ACL, CLEAN и exact head проверяет caller.
func DecodeQAReport(raw []byte) (QAReport, error) {
	var report QAReport
	if len(raw) == 0 || len(raw) > MaximumQAReportBytes || !utf8.Valid(raw) || !uniqueJSON(raw) {
		return report, ErrResultInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return report, ErrResultInvalid
	}
	names := []string{"schema", "repository", "pullRequestNumber", "headSha", "verdict", "tests", "findings"}
	if _, exists := fields["issue"]; exists {
		names = append(names, "issue")
	}
	if !exactFields(fields, names...) || decodeQAReportWire(raw, &report) != nil || report.Schema != "kodex.workflow-qa-report.v1" ||
		len(report.Repository) > 200 || !RepositoryPattern.MatchString(report.Repository) || report.Number < 1 || report.Number > 9007199254740991 ||
		!CommitPattern.MatchString(report.HeadSHA) || (report.Verdict != "PASS" && report.Verdict != "CHANGES_REQUIRED") ||
		len(report.Tests) < 1 || len(report.Tests) > 8 || len(report.Findings) > 8 {
		return QAReport{}, ErrResultInvalid
	}
	var tests []map[string]json.RawMessage
	if json.Unmarshal(fields["tests"], &tests) != nil {
		return QAReport{}, ErrResultInvalid
	}
	for index, test := range report.Tests {
		if !exactFields(tests[index], "name", "command", "exitCode", "log") || !qaText(test.Name, 160) || !qaText(test.Command, 2048) ||
			test.ExitCode < 0 || test.ExitCode > 255 || report.Verdict == "PASS" && test.ExitCode != 0 ||
			!validArtifactSource(tests[index]["log"], test.Log) || test.Log.Kind != "CAPTURED_ARTIFACT" {
			return QAReport{}, ErrResultInvalid
		}
	}
	var findings []map[string]json.RawMessage
	if json.Unmarshal(fields["findings"], &findings) != nil {
		return QAReport{}, ErrResultInvalid
	}
	seen := map[string]bool{}
	for index, finding := range report.Findings {
		if !exactFields(findings[index], "key", "summary") || !resultKey.MatchString(finding.Key) || seen[finding.Key] || !qaText(finding.Summary, 2000) {
			return QAReport{}, ErrResultInvalid
		}
		seen[finding.Key] = true
	}
	if report.Verdict == "PASS" && (len(report.Findings) != 0 || report.Issue != nil) {
		return QAReport{}, ErrResultInvalid
	}
	if report.Issue != nil {
		var issue map[string]json.RawMessage
		if json.Unmarshal(fields["issue"], &issue) != nil || !exactFields(issue, "title", "body") ||
			report.Verdict != "CHANGES_REQUIRED" || len(report.Findings) == 0 || !qaText(report.Issue.Title, 256) || !qaText(report.Issue.Body, 8192) {
			return QAReport{}, ErrResultInvalid
		}
	}
	return report, nil
}

// qaText bounds explanatory report text without trimming or interpreting it as code or authority.
// qaText ограничивает пояснения report без обрезки либо интерпретации как кода или полномочий.
func qaText(text string, maximum int) bool {
	return strings.TrimSpace(text) != "" && utf8.ValidString(text) && utf8.RuneCountInString(text) <= maximum && !strings.ContainsRune(text, 0)
}

// qaZeroInteger accepts integral zero spellings without allocating from an unbounded exponent.
// qaZeroInteger принимает целые формы нуля без выделения памяти по неограниченной экспоненте.
var qaZeroInteger = regexp.MustCompile(`^-?0(\.0+)?([eE][+-]?[0-9]+)?$`)

// decodeQAReportWire preserves numeric lexemes until exact safe-integer conversion and reuses
// the artifact selector's bounded rational parser; no float64 rounding can change report pins.
// decodeQAReportWire сохраняет числовые лексемы до exact safe-integer conversion и использует
// bounded rational parser artifact selector; округление float64 не меняет report pins.
func decodeQAReportWire(raw []byte, report *QAReport) error {
	var wire struct {
		Schema     string          `json:"schema"`
		Repository string          `json:"repository"`
		Number     json.RawMessage `json:"pullRequestNumber"`
		HeadSHA    string          `json:"headSha"`
		Verdict    string          `json:"verdict"`
		Tests      []struct {
			Name     string              `json:"name"`
			Command  string              `json:"command"`
			ExitCode json.RawMessage     `json:"exitCode"`
			Log      *artifactSourceWire `json:"log"`
		} `json:"tests"`
		Findings []QAFinding `json:"findings"`
		Issue    *QAIssue    `json:"issue,omitempty"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return ErrResultInvalid
	}
	number, valid := exactSafeInteger(bytes.TrimSpace(wire.Number), MaximumQAReportBytes)
	if !valid {
		return ErrResultInvalid
	}
	*report = QAReport{Schema: wire.Schema, Repository: wire.Repository, Number: number, HeadSHA: wire.HeadSHA, Verdict: wire.Verdict, Findings: wire.Findings, Issue: wire.Issue}
	for _, test := range wire.Tests {
		exit := bytes.TrimSpace(test.ExitCode)
		code, valid := exactSafeInteger(exit, MaximumQAReportBytes)
		if qaZeroInteger.Match(exit) {
			code, valid = 0, true
		}
		if !valid || code > 255 || test.Log == nil {
			return ErrResultInvalid
		}
		revision, revisionValid := exactSafeInteger(test.Log.Revision, MaximumQAReportBytes)
		if !revisionValid {
			return ErrResultInvalid
		}
		selector := &entity.WorkflowArtifactSource{Kind: test.Log.Kind, ArtifactRef: test.Log.ArtifactRef, Revision: revision, Digest: test.Log.Digest}
		report.Tests = append(report.Tests, QATest{Name: test.Name, Command: test.Command, ExitCode: int32(code), Log: selector})
	}
	return nil
}
