package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type githubPullRequestSnapshot struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

type outputRepairPromptTemplateData struct {
	PromptLocale     string
	MissingFields    []string
	TargetBranch     string
	ExistingPRNumber int
}

func (s *Service) resolveCodexReport(ctx context.Context, state codexState, result *runResult, outputSchemaFile string, codexOutput []byte, requiresPRFlow bool) (codexReport, []byte, error) {
	report, _, parseErr := parseCodexReportOutput(codexOutput)
	if parseErr == nil {
		report = enrichCodexReportWithLocalState(report, *result)
	}

	recoveredReport, recovered, recoverErr := s.recoverCodexReportFromGitHub(ctx, state.repoDir, *result, report, requiresPRFlow)
	if recoverErr != nil {
		s.logger.Warn("recover codex report from github failed", "run_id", s.cfg.RunID, "err", recoverErr)
	}
	if recovered {
		report = recoveredReport
	}
	report = enrichCodexReportWithLocalState(report, *result)
	if missing := missingCriticalCodexReportFields(report, requiresPRFlow); len(missing) == 0 {
		return report, nil, nil
	}

	repairOutput, repairErr := s.repairCodexStructuredOutput(ctx, state, *result, outputSchemaFile, report, requiresPRFlow)
	if repairErr != nil {
		if parseErr != nil {
			return codexReport{}, nil, fmt.Errorf("parse codex structured output: %w", parseErr)
		}
		return codexReport{}, nil, repairErr
	}

	repairedReport, _, err := parseCodexReportOutput(repairOutput)
	if err != nil {
		return codexReport{}, nil, fmt.Errorf("parse repaired codex structured output: %w", err)
	}
	repairedReport = enrichCodexReportWithLocalState(repairedReport, *result)

	recoveredReport, recovered, recoverErr = s.recoverCodexReportFromGitHub(ctx, state.repoDir, *result, repairedReport, requiresPRFlow)
	if recoverErr != nil {
		s.logger.Warn("recover repaired codex report from github failed", "run_id", s.cfg.RunID, "err", recoverErr)
	}
	if recovered {
		repairedReport = recoveredReport
	}
	repairedReport = enrichCodexReportWithLocalState(repairedReport, *result)
	if missing := missingCriticalCodexReportFields(repairedReport, requiresPRFlow); len(missing) > 0 {
		return codexReport{}, nil, fmt.Errorf("invalid codex result: missing required fields after repair: %s", strings.Join(missing, ", "))
	}
	return repairedReport, repairOutput, nil
}

func enrichCodexReportWithLocalState(report codexReport, result runResult) codexReport {
	if strings.TrimSpace(report.Branch) == "" {
		report.Branch = strings.TrimSpace(result.targetBranch)
	}
	if strings.TrimSpace(report.SessionID) == "" {
		report.SessionID = strings.TrimSpace(result.sessionID)
	}
	if report.PRNumber <= 0 && result.existingPRNumber > 0 {
		report.PRNumber = result.existingPRNumber
	}
	if strings.TrimSpace(report.PRURL) == "" {
		report.PRURL = strings.TrimSpace(result.prURL)
	}
	return report
}

func missingCriticalCodexReportFields(report codexReport, requiresPRFlow bool) []string {
	fields := make([]string, 0, 2)
	if !requiresPRFlow {
		return fields
	}
	if report.PRNumber <= 0 {
		fields = append(fields, outputFieldPRNumber)
	}
	if strings.TrimSpace(report.PRURL) == "" {
		fields = append(fields, outputFieldPRURL)
	}
	sort.Strings(fields)
	return fields
}

func (s *Service) recoverCodexReportFromGitHub(ctx context.Context, repoDir string, result runResult, report codexReport, requiresPRFlow bool) (codexReport, bool, error) {
	if !requiresPRFlow {
		return report, false, nil
	}
	recovered := false
	prNumber := report.PRNumber
	prURL := strings.TrimSpace(report.PRURL)

	if prNumber <= 0 && result.existingPRNumber > 0 {
		prNumber = result.existingPRNumber
		recovered = true
	}

	if prNumber > 0 && prURL == "" {
		snapshot, found, err := lookupPullRequestByNumber(ctx, repoDir, prNumber)
		if err != nil {
			return report, recovered, err
		}
		if found && strings.TrimSpace(snapshot.URL) != "" {
			prURL = strings.TrimSpace(snapshot.URL)
			recovered = true
		}
	}

	if prNumber <= 0 || prURL == "" {
		snapshot, found, err := lookupPullRequestByBranch(ctx, repoDir, result.targetBranch)
		if err != nil {
			return report, recovered, err
		}
		if found {
			if prNumber <= 0 && snapshot.Number > 0 {
				prNumber = snapshot.Number
				recovered = true
			}
			if prURL == "" && strings.TrimSpace(snapshot.URL) != "" {
				prURL = strings.TrimSpace(snapshot.URL)
				recovered = true
			}
		}
	}

	if !recovered {
		return report, false, nil
	}
	report.PRNumber = prNumber
	report.PRURL = prURL
	return report, true, nil
}

func lookupPullRequestByNumber(ctx context.Context, repoDir string, prNumber int) (githubPullRequestSnapshot, bool, error) {
	if prNumber <= 0 {
		return githubPullRequestSnapshot{}, false, nil
	}
	output, err := runCommandCaptureCombinedOutput(ctx, repoDir, "gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "number,url,state")
	if err != nil {
		return githubPullRequestSnapshot{}, false, fmt.Errorf("gh pr view %d: %w", prNumber, err)
	}
	var snapshot githubPullRequestSnapshot
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		return githubPullRequestSnapshot{}, false, fmt.Errorf("decode gh pr view %d: %w", prNumber, err)
	}
	if snapshot.Number <= 0 {
		return githubPullRequestSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

func lookupPullRequestByBranch(ctx context.Context, repoDir string, branch string) (githubPullRequestSnapshot, bool, error) {
	trimmedBranch := strings.TrimSpace(branch)
	if trimmedBranch == "" {
		return githubPullRequestSnapshot{}, false, nil
	}
	output, err := runCommandCaptureCombinedOutput(ctx, repoDir, "gh", "pr", "list", "--head", trimmedBranch, "--state", "all", "--limit", "20", "--json", "number,url,state")
	if err != nil {
		return githubPullRequestSnapshot{}, false, fmt.Errorf("gh pr list --head %s: %w", trimmedBranch, err)
	}
	var items []githubPullRequestSnapshot
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return githubPullRequestSnapshot{}, false, fmt.Errorf("decode gh pr list for branch %s: %w", trimmedBranch, err)
	}
	if len(items) == 0 {
		return githubPullRequestSnapshot{}, false, nil
	}
	return items[0], true, nil
}

func (s *Service) repairCodexStructuredOutput(ctx context.Context, state codexState, result runResult, outputSchemaFile string, report codexReport, requiresPRFlow bool) ([]byte, error) {
	repairPrompt, err := renderTemplate(templateNamePromptOutputRepair, outputRepairPromptTemplateData{
		PromptLocale:     normalizePromptLocale(s.cfg.PromptTemplateLocale),
		MissingFields:    missingCriticalCodexReportFields(report, requiresPRFlow),
		TargetBranch:     result.targetBranch,
		ExistingPRNumber: maxInt(result.existingPRNumber, report.PRNumber),
	})
	if err != nil {
		return nil, fmt.Errorf("render output repair prompt: %w", err)
	}

	repairOutput, err := s.runCodexExecWithAuthRecovery(ctx, state, codexExecParams{
		RepoDir:          state.repoDir,
		Resume:           result.restoredSessionPath != "" || result.sessionID != "" || result.sessionFilePath != "",
		ResumeSessionID:  result.sessionID,
		OutputSchemaFile: outputSchemaFile,
		Prompt:           strings.TrimSpace(repairPrompt),
	})
	if err != nil {
		return nil, fmt.Errorf("codex structured output repair failed: %w", err)
	}
	return repairOutput, nil
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
