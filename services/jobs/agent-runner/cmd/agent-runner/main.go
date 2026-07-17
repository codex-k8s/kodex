package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	workspaceDir     = "/workspace"
	repoDir          = "/workspace/repo"
	artifactsDir     = "/workspace/artifacts"
	codexHomeDir     = "/workspace/codex-home"
	codexAuthDir     = "/codex-home"
	promptPath       = "/var/run/matter-codex-prompt/prompt.md"
	codexAuthPath    = "/var/run/secrets/matter-codex-codex/auth.json"
	gitHubTokenPath  = "/var/run/secrets/matter-codex-github/github-token"
	gitHubUserPath   = "/var/run/secrets/matter-codex-github/github-username"
	gitHubEmailPath  = "/var/run/secrets/matter-codex-github/github-email"
	runnerBinaryPath = "/usr/local/bin/matter-codex-agent-runner"
)

const (
	codexAuthCheckPrompt        = "ping: answer with exactly one word, pong. Do not use tools."
	sessionAPIMaxAttempts       = 30
	sessionAPIInitialRetryDelay = time.Second
	sessionAPIMaxRetryDelay     = 10 * time.Second
	capacityRetryPhase          = "capacity_retry"
	capacityRetryExhaustedKey   = "codex-capacity-retries-exhausted"
	capacityRetryCountKey       = "codex-capacity-retry-count"
	failureCodeKey              = "failure-code"
	providerPolicyBlockedCode   = "provider-policy-blocked"
	providerPolicyBlockedStatus = "blocked"
)

var codexCapacityRetryDelays = []time.Duration{time.Minute, 3 * time.Minute, 5 * time.Minute}

type sessionAPIStatusError struct {
	StatusCode int
	Body       string
}

func (err sessionAPIStatusError) Error() string {
	return fmt.Sprintf("bot-service session API returned %d: %s", err.StatusCode, err.Body)
}

type runner struct {
	failureLogs []string
}

type githubAccount struct {
	Token    string
	Username string
	Email    string
}

type sessionSnapshotResponse struct {
	SessionKey               string `json:"session_key"`
	CodexSessionID           string `json:"codex_session_id"`
	SessionArchiveGzipBase64 string `json:"session_archive_gzip_base64"`
	ExpiresAt                string `json:"expires_at"`
}

type sessionTurnClaimResponse struct {
	HasTurn        bool   `json:"has_turn"`
	Exit           bool   `json:"exit"`
	TurnID         int64  `json:"turn_id"`
	RunID          string `json:"run_id"`
	Prompt         string `json:"prompt"`
	CodexSessionID string `json:"codex_session_id"`
	ExpiresAt      string `json:"expires_at"`
}

type sessionTurnCompleteRequest struct {
	TurnID                   int64             `json:"turn_id"`
	RunID                    string            `json:"run_id"`
	Status                   string            `json:"status"`
	FinalMessage             string            `json:"final_message"`
	ErrorMessage             string            `json:"error_message"`
	CodexSessionID           string            `json:"codex_session_id"`
	SessionArchiveGzipBase64 string            `json:"session_archive_gzip_base64"`
	Artifacts                map[string]string `json:"artifacts"`
}

type sessionTurnStatusRequest struct {
	RunID             string `json:"run_id"`
	Phase             string `json:"phase"`
	OpenAIAccount     string `json:"openai_account,omitempty"`
	CodexLimits       string `json:"codex_limits,omitempty"`
	RetryAttempt      int    `json:"retry_attempt,omitempty"`
	RetryMaxAttempts  int    `json:"retry_max_attempts,omitempty"`
	RetryDelaySeconds int    `json:"retry_delay_seconds,omitempty"`
}

func main() {
	defer func() {
		if recovered := recover(); recovered != nil {
			fail(fmt.Errorf("%v", recovered), nil)
		}
	}()
	if os.Getenv("MATTERCODEX_GIT_ASKPASS") == "1" {
		runGitAskPass()
		return
	}
	if len(os.Args) < 2 {
		fail(errors.New("mode is required"), nil)
	}
	ctx := context.Background()
	r := &runner{}
	var err error
	switch os.Args[1] {
	case "smoke":
		err = r.runSmoke()
	case "codex-auth":
		err = r.runCodexAuth(ctx)
	case "auth-ready-check":
		err = authReadyCheck()
	case "codex-auth-secret-check":
		err = r.runCodexAuthSecretCheck(ctx)
	case "print-auth-json":
		err = printAuthJSON()
	case "developer":
		err = r.runDeveloper(ctx)
	case "reviewer":
		err = r.runReviewer(ctx)
	case "chat":
		err = r.runChat(ctx)
	case "session":
		err = r.runSession(ctx)
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fail(err, r.failureLogs)
	}
}

func (r *runner) runSmoke() error {
	runID := requiredEnv("MATTERCODEX_RUN_ID")
	role := defaultString(os.Getenv("MATTERCODEX_AGENT_ROLE"), "smoke")
	fmt.Println("matter-codex smoke start")
	fmt.Printf("run-id: %s\n", runID)
	fmt.Printf("agent-role: %s\n", role)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "smoke.txt"), []byte("smoke-ok\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("smoke-ok")
	fmt.Println("matter-codex smoke done")
	return nil
}

func (r *runner) runCodexAuth(ctx context.Context) error {
	account := requiredEnv("MATTERCODEX_OPENAI_ACCOUNT")
	fmt.Println("matter-codex codex auth start")
	fmt.Printf("account: %s\n", account)
	if err := os.MkdirAll(codexAuthDir, 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "codex", "login", "--device-auth")
	cmd.Env = mergeEnv(os.Environ(), "CODEX_HOME="+codexAuthDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codex login: %w", err)
	}
	if err := authJSONCheck(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(codexAuthDir, ".auth-ready"), []byte("ready\n"), 0o600); err != nil {
		return err
	}
	fmt.Println("matter-codex codex auth ready")
	time.Sleep(15 * time.Minute)
	return nil
}

func (r *runner) runCodexAuthSecretCheck(ctx context.Context) error {
	fmt.Println("matter-codex codex auth secret check start")
	if err := r.prepareWorkspace(); err != nil {
		return err
	}
	// Authentication checks must validate only the saved Codex login. Role-level
	// config overlays are validated by real agent runs; mixing them here turns
	// config errors into misleading reauth requests.
	if err := disableCodexConfigOverlayForAuthCheck(); err != nil {
		return err
	}
	if err := r.prepareCodexHome(ctx); err != nil {
		return err
	}
	checkDir := filepath.Join(workspaceDir, "codex-auth-check")
	if err := os.MkdirAll(checkDir, 0o755); err != nil {
		return err
	}
	if err := r.runLogged(ctx, "", []string{"CODEX_HOME=" + codexHomeDir}, "codex-auth-ping.log", "codex", "exec", "--skip-git-repo-check", "--cd", checkDir, codexAuthCheckPrompt); err != nil {
		return fmt.Errorf("codex auth ping failed: %w", err)
	}
	fmt.Println("matter-codex codex auth secret check ready")
	return nil
}

func (r *runner) runDeveloper(ctx context.Context) error {
	runID := requiredEnv("MATTERCODEX_RUN_ID")
	profile := requiredEnv("MATTERCODEX_AGENT_PROFILE")
	owner := requiredEnv("MATTERCODEX_REPO_OWNER")
	name := requiredEnv("MATTERCODEX_REPO_NAME")
	baseBranch := requiredEnv("MATTERCODEX_BASE_BRANCH")
	headBranch := requiredEnv("MATTERCODEX_HEAD_BRANCH")
	prTitle := requiredEnv("MATTERCODEX_PR_TITLE")
	repo := owner + "/" + name

	fmt.Println("matter-codex developer run start")
	fmt.Printf("run-id: %s\n", runID)
	fmt.Printf("profile: %s\n", profile)
	if err := r.prepareWorkspace(); err != nil {
		return err
	}
	if err := r.prepareCodexHome(ctx); err != nil {
		return err
	}
	account, err := readGitHubAccount()
	if err != nil {
		return err
	}
	githubEnv := account.env()
	if err := r.runLogged(ctx, "", githubEnv, "git-clone.log", "git", "clone", "https://github.com/"+repo+".git", repoDir); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-checkout.log", "git", "checkout", "-B", headBranch, "origin/"+baseBranch); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-config-name.log", "git", "config", "user.name", account.Username); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-config-email.log", "git", "config", "user.email", account.Email); err != nil {
		return err
	}
	if err := r.runCodexExec(ctx, repoDir, "codex-final.md", githubEnv); err != nil {
		return err
	}
	r.printFinalAnswer("codex-final.md")
	changed, err := r.gitHasChanges(ctx)
	if err != nil {
		return err
	}
	if !changed {
		artifact("no-changes", "true")
		fmt.Println("matter-codex developer run done")
		return nil
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-add.log", "git", "add", "-A"); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-commit.log", "git", "commit", "-m", "Apply matter-codex developer run "+runID); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-push.log", "git", "push", "origin", headBranch); err != nil {
		return err
	}
	bodyPath, err := r.writeDeveloperPRBody(runID, profile, baseBranch, headBranch)
	if err != nil {
		return err
	}
	prURL, err := r.capture(ctx, repoDir, githubEnv, "gh-pr-view.log", "gh", "pr", "view", headBranch, "--repo", repo, "--json", "url", "--jq", ".url")
	if err != nil {
		prURL, err = r.capture(ctx, repoDir, githubEnv, "gh-pr-create.log", "gh", "pr", "create", "--repo", repo, "--base", baseBranch, "--head", headBranch, "--title", prTitle, "--body-file", bodyPath, "--draft")
		if err != nil {
			return err
		}
	}
	artifact("pr-url", strings.TrimSpace(prURL))
	artifact("branch", headBranch)
	commit, err := r.capture(ctx, repoDir, nil, "git-rev-parse.log", "git", "rev-parse", "--short", "HEAD")
	if err == nil {
		artifact("commit", strings.TrimSpace(commit))
	}
	fmt.Println("matter-codex developer run done")
	return nil
}

func (r *runner) runReviewer(ctx context.Context) error {
	runID := requiredEnv("MATTERCODEX_RUN_ID")
	profile := requiredEnv("MATTERCODEX_AGENT_PROFILE")
	owner := requiredEnv("MATTERCODEX_REPO_OWNER")
	name := requiredEnv("MATTERCODEX_REPO_NAME")
	prNumber := requiredEnv("MATTERCODEX_PR_NUMBER")
	repo := owner + "/" + name

	fmt.Println("matter-codex reviewer run start")
	fmt.Printf("run-id: %s\n", runID)
	fmt.Printf("profile: %s\n", profile)
	fmt.Printf("repository: %s\n", repo)
	fmt.Printf("pr-number: %s\n", prNumber)
	if err := r.prepareWorkspace(); err != nil {
		return err
	}
	if err := r.prepareCodexHome(ctx); err != nil {
		return err
	}
	account, err := readGitHubAccount()
	if err != nil {
		return err
	}
	githubEnv := account.env()
	prURL, err := r.capture(ctx, "", githubEnv, "gh-pr-url.log", "gh", "pr", "view", prNumber, "--repo", repo, "--json", "url", "--jq", ".url")
	if err != nil {
		return err
	}
	if err := r.runLogged(ctx, "", githubEnv, "git-clone.log", "git", "clone", "https://github.com/"+repo+".git", repoDir); err != nil {
		return err
	}
	branchName := "review-pr-" + prNumber
	if err := r.runLogged(ctx, repoDir, githubEnv, "git-fetch.log", "git", "fetch", "origin", "pull/"+prNumber+"/head:"+branchName); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, nil, "git-checkout.log", "git", "checkout", branchName); err != nil {
		return err
	}
	if err := r.runCodexExec(ctx, repoDir, "review-final.md", githubEnv); err != nil {
		return err
	}
	r.printFinalAnswer("review-final.md")
	decision := normalizeDecision(readDecision(filepath.Join(artifactsDir, "review-final.md")))
	submittedByAgent := readReviewSubmitted(filepath.Join(artifactsDir, "review-final.md"))
	bodyPath, err := r.writeReviewBody(runID, prNumber, "review-final.md")
	if err != nil {
		return err
	}
	if !submittedByAgent {
		flag := reviewFlag(decision)
		if err := r.runLogged(ctx, repoDir, githubEnv, "gh-pr-review.log", "gh", "pr", "review", prNumber, "--repo", repo, flag, "--body-file", bodyPath); err != nil {
			if flag == "--comment" {
				return err
			}
			decision = "comment"
			fallbackPath, fallbackErr := r.writeReviewFallbackBody(runID, prNumber, flag, "review-final.md")
			if fallbackErr != nil {
				return fallbackErr
			}
			if err := r.runLogged(ctx, repoDir, githubEnv, "gh-pr-review-fallback.log", "gh", "pr", "review", prNumber, "--repo", repo, "--comment", "--body-file", fallbackPath); err != nil {
				return err
			}
		}
	}
	artifact("pr-url", strings.TrimSpace(prURL))
	artifact("review-decision", decision)
	artifact("review-submitted", "true")
	changed, err := r.gitHasChanges(ctx)
	if err == nil && changed {
		artifact("local-changes", "true")
	}
	fmt.Println("matter-codex reviewer run done")
	return nil
}

func (r *runner) runChat(ctx context.Context) error {
	runID := requiredEnv("MATTERCODEX_RUN_ID")
	profile := requiredEnv("MATTERCODEX_AGENT_PROFILE")

	fmt.Println("matter-codex chat run start")
	fmt.Printf("run-id: %s\n", runID)
	fmt.Printf("profile: %s\n", profile)
	if err := r.prepareWorkspace(); err != nil {
		return err
	}
	if err := r.prepareCodexHome(ctx); err != nil {
		return err
	}
	extraEnv := []string{}
	if os.Getenv("MATTERCODEX_GITHUB_ENABLED") == "true" {
		account, err := readGitHubAccount()
		if err != nil {
			return err
		}
		extraEnv = account.env()
	}
	if err := r.runCodexExec(ctx, workspaceDir, "chat-final.md", extraEnv); err != nil {
		return err
	}
	r.printFinalAnswer("chat-final.md")
	fmt.Println("matter-codex chat run done")
	return nil
}

func (r *runner) runSession(ctx context.Context) error {
	sessionKey := requiredEnv("MATTERCODEX_SESSION_KEY")
	profile := requiredEnv("MATTERCODEX_AGENT_PROFILE")
	botServiceURL := strings.TrimRight(requiredEnv("MATTERCODEX_BOT_SERVICE_URL"), "/")
	sessionToken := requiredEnv("MATTERCODEX_SESSION_TOKEN")
	openAIAccount := strings.TrimSpace(os.Getenv("MATTERCODEX_OPENAI_ACCOUNT"))

	fmt.Println("matter-codex session runner start")
	fmt.Printf("session-key: %s\n", sessionKey)
	fmt.Printf("profile: %s\n", profile)
	if err := r.prepareWorkspace(); err != nil {
		return err
	}
	if err := r.prepareCodexHome(ctx); err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	snapshot, err := r.fetchSessionSnapshot(ctx, client, botServiceURL, sessionKey, sessionToken)
	if err != nil {
		return err
	}
	if err := restoreCodexSessionArchive(snapshot.SessionArchiveGzipBase64, codexHomeDir); err != nil {
		return err
	}
	extraEnv := []string{}
	workDir := workspaceDir
	if os.Getenv("MATTERCODEX_GITHUB_ENABLED") == "true" {
		account, err := readGitHubAccount()
		if err != nil {
			return err
		}
		extraEnv = account.env()
		if os.Getenv("MATTERCODEX_SESSION_REPOSITORY_ENABLED") == "true" {
			if err := r.prepareSessionRepository(ctx, account, extraEnv); err != nil {
				return err
			}
			workDir = repoDir
		}
	}
	codexSessionID := strings.TrimSpace(snapshot.CodexSessionID)
	for {
		claim, err := r.claimSessionTurn(ctx, client, botServiceURL, sessionKey, sessionToken)
		if err != nil {
			return err
		}
		if claim.Exit {
			fmt.Println("matter-codex session idle ttl expired")
			return nil
		}
		if !claim.HasTurn {
			time.Sleep(10 * time.Second)
			continue
		}
		if strings.TrimSpace(claim.CodexSessionID) != "" {
			codexSessionID = strings.TrimSpace(claim.CodexSessionID)
		}
		if err := r.updateSessionTurnStatus(ctx, client, botServiceURL, sessionKey, sessionToken, sessionTurnStatusRequest{
			RunID:         claim.RunID,
			Phase:         "running",
			OpenAIAccount: openAIAccount,
			CodexLimits:   r.latestCodexLimitsSummary(),
		}); err != nil {
			fmt.Printf("matter-codex session status update skipped: %v\n", err)
		}
		finalFile := fmt.Sprintf("session-turn-%d-final.md", claim.TurnID)
		retryCount := 0
		nextSessionID, finalMessage, runErr := r.runCodexSessionTurn(ctx, claim, codexSessionID, finalFile, workDir, extraEnv, retryCount)
		if strings.TrimSpace(nextSessionID) != "" {
			codexSessionID = strings.TrimSpace(nextSessionID)
		}
		for retryIndex, delay := range codexCapacityRetryDelays {
			if !codexTransientCapacityFailure(sessionTurnEventsPath(claim.TurnID, retryCount), sessionTurnStderrPath(claim.TurnID, retryCount), runErr) {
				break
			}
			retryCount = retryIndex + 1
			if err := r.updateSessionTurnStatus(ctx, client, botServiceURL, sessionKey, sessionToken, sessionTurnStatusRequest{
				RunID:             claim.RunID,
				Phase:             capacityRetryPhase,
				OpenAIAccount:     openAIAccount,
				CodexLimits:       r.latestCodexLimitsSummary(),
				RetryAttempt:      retryCount,
				RetryMaxAttempts:  len(codexCapacityRetryDelays),
				RetryDelaySeconds: int(delay / time.Second),
			}); err != nil {
				fmt.Printf("matter-codex capacity retry status update skipped: %v\n", err)
			}
			if err := waitCodexCapacityRetry(ctx, delay); err != nil {
				runErr = err
				break
			}
			if err := r.updateSessionTurnStatus(ctx, client, botServiceURL, sessionKey, sessionToken, sessionTurnStatusRequest{
				RunID:         claim.RunID,
				Phase:         "running",
				OpenAIAccount: openAIAccount,
				CodexLimits:   r.latestCodexLimitsSummary(),
			}); err != nil {
				fmt.Printf("matter-codex capacity retry start status update skipped: %v\n", err)
			}
			retryClaim := claim
			if strings.TrimSpace(codexSessionID) != "" {
				retryClaim.Prompt = codexCapacityRetryPrompt(retryCount, len(codexCapacityRetryDelays))
			}
			nextSessionID, finalMessage, runErr = r.runCodexSessionTurn(ctx, retryClaim, codexSessionID, finalFile, workDir, extraEnv, retryCount)
			if strings.TrimSpace(nextSessionID) != "" {
				codexSessionID = strings.TrimSpace(nextSessionID)
			}
		}
		capacityRetriesExhausted := runErr != nil && retryCount == len(codexCapacityRetryDelays) &&
			codexTransientCapacityFailure(sessionTurnEventsPath(claim.TurnID, retryCount), sessionTurnStderrPath(claim.TurnID, retryCount), runErr)
		providerPolicyBlocked := codexProviderPolicyFailure(sessionTurnEventsPath(claim.TurnID, retryCount), sessionTurnStderrPath(claim.TurnID, retryCount), runErr)
		archive, snapshotErr := createCodexSessionArchive(codexHomeDir)
		status := "succeeded"
		errorMessage := ""
		if runErr != nil {
			status = "failed"
			errorMessage = runErr.Error()
		}
		if snapshotErr != nil {
			status = "failed"
			if errorMessage != "" {
				errorMessage += "; "
			}
			errorMessage += snapshotErr.Error()
		}
		artifacts := map[string]string{}
		if openAIAccount != "" {
			artifacts["openai-account"] = openAIAccount
		}
		if limits := r.latestCodexLimitsSummary(); limits != "" {
			artifacts["codex-limits"] = limits
		}
		if retryCount > 0 {
			artifacts[capacityRetryCountKey] = strconv.Itoa(retryCount)
		}
		if capacityRetriesExhausted {
			artifacts[capacityRetryExhaustedKey] = "true"
			errorMessage = fmt.Sprintf("Codex model remained at capacity after %d automatic retries", retryCount)
		}
		if providerPolicyBlocked {
			status = providerPolicyBlockedStatus
			artifacts[failureCodeKey] = providerPolicyBlockedCode
			errorMessage = "Codex request was blocked by the provider cyber safety policy"
			finalMessage = ""
		}
		if err := r.completeSessionTurn(ctx, client, botServiceURL, sessionKey, sessionToken, sessionTurnCompleteRequest{
			TurnID:                   claim.TurnID,
			RunID:                    claim.RunID,
			Status:                   status,
			FinalMessage:             finalMessage,
			ErrorMessage:             errorMessage,
			CodexSessionID:           codexSessionID,
			SessionArchiveGzipBase64: archive,
			Artifacts:                artifacts,
		}); err != nil {
			return err
		}
	}
}

func (r *runner) prepareSessionRepository(ctx context.Context, account githubAccount, githubEnv []string) error {
	provider := defaultString(os.Getenv("MATTERCODEX_REPO_PROVIDER"), "github")
	if provider != "github" {
		return fmt.Errorf("unsupported session repository provider %q", provider)
	}
	owner := requiredEnv("MATTERCODEX_REPO_OWNER")
	name := requiredEnv("MATTERCODEX_REPO_NAME")
	branch := defaultString(os.Getenv("MATTERCODEX_BASE_BRANCH"), "main")
	repo := owner + "/" + name
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		if err := r.runLogged(ctx, repoDir, githubEnv, "session-git-remote.log", "git", "remote", "set-url", "origin", "https://github.com/"+repo+".git"); err != nil {
			return err
		}
		if err := r.runLogged(ctx, repoDir, githubEnv, "session-git-fetch.log", "git", "fetch", "--prune", "origin"); err != nil {
			return err
		}
	} else {
		if err := r.runLogged(ctx, "", githubEnv, "session-git-clone.log", "git", "clone", "https://github.com/"+repo+".git", repoDir); err != nil {
			return err
		}
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "session-git-checkout.log", "git", "checkout", "-B", branch, "origin/"+branch); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "session-git-config-name.log", "git", "config", "user.name", account.Username); err != nil {
		return err
	}
	if err := r.runLogged(ctx, repoDir, githubEnv, "session-git-config-email.log", "git", "config", "user.email", account.Email); err != nil {
		return err
	}
	return nil
}

func (r *runner) prepareWorkspace() error {
	for _, dir := range []string{repoDir, artifactsDir, codexHomeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) prepareCodexHome(ctx context.Context) error {
	if err := writeCodexConfig(filepath.Join(codexHomeDir, "config.toml")); err != nil {
		return err
	}
	if err := copyFile(codexAuthPath, filepath.Join(codexHomeDir, "auth.json"), 0o600); err != nil {
		return err
	}
	if err := r.runLogged(ctx, "", []string{"CODEX_HOME=" + codexHomeDir}, "codex-login-status.log", "codex", "login", "status"); err != nil {
		return err
	}
	_ = r.runLogged(ctx, "", []string{"CODEX_HOME=" + codexHomeDir}, "mcp-list.log", "codex", "mcp", "list")
	return nil
}

func (r *runner) runCodexExec(ctx context.Context, workDir string, finalFile string, extraEnv []string) error {
	promptFile, err := os.Open(promptPath)
	if err != nil {
		return err
	}
	defer promptFile.Close()
	events, err := os.Create(filepath.Join(artifactsDir, "codex-events.jsonl"))
	if err != nil {
		return err
	}
	defer events.Close()
	stderr, err := os.Create(filepath.Join(artifactsDir, "codex-stderr.log"))
	if err != nil {
		return err
	}
	defer stderr.Close()
	r.failureLogs = append(r.failureLogs, filepath.Join(artifactsDir, "codex-stderr.log"))
	cmd := exec.CommandContext(ctx, "codex", "exec", "--json", "--cd", workDir, "--sandbox", codexSandboxMode(), "--skip-git-repo-check", "--output-last-message", filepath.Join(artifactsDir, finalFile), "-")
	cmd.Env = mergeEnv(os.Environ(), append(extraEnv, "CODEX_HOME="+codexHomeDir)...)
	cmd.Stdin = promptFile
	cmd.Stdout = events
	cmd.Stderr = stderr
	return cmd.Run()
}

func (r *runner) runCodexSessionTurn(ctx context.Context, claim sessionTurnClaimResponse, codexSessionID string, finalFile string, workDir string, extraEnv []string, attempt int) (string, string, error) {
	eventsPath := sessionTurnEventsPath(claim.TurnID, attempt)
	stderrPath := sessionTurnStderrPath(claim.TurnID, attempt)
	finalPath := filepath.Join(artifactsDir, finalFile)
	_ = os.Remove(finalPath)
	events, err := os.Create(eventsPath)
	if err != nil {
		return codexSessionID, "", err
	}
	defer events.Close()
	stderr, err := os.Create(stderrPath)
	if err != nil {
		return codexSessionID, "", err
	}
	defer stderr.Close()
	r.failureLogs = append(r.failureLogs, stderrPath)

	args := []string{"exec"}
	if strings.TrimSpace(codexSessionID) != "" {
		args = append(args, "resume", "--json", "--skip-git-repo-check", "--output-last-message", finalPath, codexSessionID, "-")
	} else {
		args = append(args, "--json", "--cd", workDir, "--sandbox", codexSandboxMode(), "--skip-git-repo-check", "--output-last-message", finalPath, "-")
	}
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = workDir
	cmd.Env = mergeEnv(os.Environ(), append(extraEnv, "CODEX_HOME="+codexHomeDir)...)
	cmd.Stdin = strings.NewReader(claim.Prompt)
	cmd.Stdout = events
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if discovered := readCodexSessionID(eventsPath); discovered != "" {
		codexSessionID = discovered
	}
	finalMessage := readTextFile(finalPath)
	if finalMessage == "" && runErr != nil {
		finalMessage = tailText(stderrPath, 80)
	}
	return codexSessionID, finalMessage, runErr
}

func sessionTurnEventsPath(turnID int64, attempt int) string {
	return filepath.Join(artifactsDir, sessionTurnArtifactName("codex-events", turnID, attempt, ".jsonl"))
}

func sessionTurnStderrPath(turnID int64, attempt int) string {
	return filepath.Join(artifactsDir, sessionTurnArtifactName("codex-stderr", turnID, attempt, ".log"))
}

func sessionTurnArtifactName(prefix string, turnID int64, attempt int, extension string) string {
	if attempt <= 0 {
		return fmt.Sprintf("%s-%d%s", prefix, turnID, extension)
	}
	return fmt.Sprintf("%s-%d-retry-%d%s", prefix, turnID, attempt, extension)
}

func codexTransientCapacityFailure(eventsPath string, stderrPath string, runErr error) bool {
	if runErr == nil {
		return false
	}
	if codexEventFileContainsTransientCapacity(eventsPath) {
		return true
	}
	body, err := os.ReadFile(stderrPath)
	return err == nil && codexTransientCapacityMessage(string(body))
}

func codexEventFileContainsTransientCapacity(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Type != "error" && event.Type != "turn.failed" {
			continue
		}
		if codexTransientCapacityMessage(event.Message) {
			return true
		}
	}
	return false
}

func codexTransientCapacityMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{
		"selected model is at capacity",
		"model is at capacity",
		"model is currently at capacity",
		"server is overloaded",
		"service is temporarily overloaded",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func codexProviderPolicyFailure(eventsPath string, stderrPath string, runErr error) bool {
	if runErr == nil {
		return false
	}
	if codexEventFileContainsProviderPolicyBlock(eventsPath) {
		return true
	}
	body, err := os.ReadFile(stderrPath)
	return err == nil && codexProviderPolicyMessage(string(body))
}

func codexEventFileContainsProviderPolicyBlock(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		if eventType != "error" && eventType != "turn.failed" {
			continue
		}
		for _, message := range codexErrorEventMessages(event) {
			if codexProviderPolicyMessage(message) {
				return true
			}
		}
	}
	return false
}

func codexErrorEventMessages(event map[string]any) []string {
	values := []string{}
	var collect func(any)
	collect = func(current any) {
		switch typed := current.(type) {
		case string:
			values = append(values, typed)
		case []any:
			for _, item := range typed {
				collect(item)
			}
		case map[string]any:
			for _, item := range typed {
				collect(item)
			}
		}
	}
	for _, key := range []string{"message", "error", "details"} {
		collect(event[key])
	}
	return values
}

func codexProviderPolicyMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{
		"flagged for possible cybersecurity risk",
		"blocked due to cybersecurity",
		"blocked by our cyber safety",
		"cyber safety classifier",
		"trusted access for cyber",
		"chatgpt.com/cyber",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func codexCapacityRetryPrompt(attempt int, maxAttempts int) string {
	return fmt.Sprintf("Continue the same turn after a transient model-capacity interruption (automatic retry %d/%d). Do not restart work that is already complete. Inspect the current workspace and conversation state, finish the original task, and follow the existing locale and language requirements.", attempt, maxAttempts)
}

func waitCodexCapacityRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *runner) fetchSessionSnapshot(ctx context.Context, client *http.Client, baseURL string, sessionKey string, token string) (sessionSnapshotResponse, error) {
	var response sessionSnapshotResponse
	if err := r.sessionJSON(ctx, client, http.MethodGet, baseURL, sessionKey, token, "snapshot", nil, &response); err != nil {
		return sessionSnapshotResponse{}, err
	}
	return response, nil
}

func (r *runner) claimSessionTurn(ctx context.Context, client *http.Client, baseURL string, sessionKey string, token string) (sessionTurnClaimResponse, error) {
	var response sessionTurnClaimResponse
	if err := r.sessionJSON(ctx, client, http.MethodPost, baseURL, sessionKey, token, "turns/claim", nil, &response); err != nil {
		return sessionTurnClaimResponse{}, err
	}
	return response, nil
}

func (r *runner) completeSessionTurn(ctx context.Context, client *http.Client, baseURL string, sessionKey string, token string, payload sessionTurnCompleteRequest) error {
	return r.sessionJSON(ctx, client, http.MethodPost, baseURL, sessionKey, token, "turns/complete", payload, nil)
}

func (r *runner) updateSessionTurnStatus(ctx context.Context, client *http.Client, baseURL string, sessionKey string, token string, payload sessionTurnStatusRequest) error {
	return r.sessionJSON(ctx, client, http.MethodPost, baseURL, sessionKey, token, "turns/status", payload, nil)
}

func (r *runner) latestCodexLimitsSummary() string {
	summary, err := latestCodexLimitsSummary(codexHomeDir)
	if err != nil {
		fmt.Printf("matter-codex codex limits unavailable: %v\n", err)
		return ""
	}
	return summary
}

func (r *runner) sessionJSON(ctx context.Context, client *http.Client, method string, baseURL string, sessionKey string, token string, action string, payload any, target any) error {
	delay := sessionAPIInitialRetryDelay
	for attempt := 1; attempt <= sessionAPIMaxAttempts; attempt++ {
		err := r.sessionJSONOnce(ctx, client, method, baseURL, sessionKey, token, action, payload, target)
		if err == nil {
			return nil
		}
		if !sessionAPIErrorRetriable(err) || attempt == sessionAPIMaxAttempts {
			return err
		}
		fmt.Printf("matter-codex session API transient error on %s, retry %d/%d in %s: %v\n", action, attempt+1, sessionAPIMaxAttempts, delay, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > sessionAPIMaxRetryDelay {
			delay = sessionAPIMaxRetryDelay
		}
	}
	return nil
}

func (r *runner) sessionJSONOnce(ctx context.Context, client *http.Client, method string, baseURL string, sessionKey string, token string, action string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, baseURL+"/internal/agent-sessions/"+sessionKey+"/"+action, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return sessionAPIStatusError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func sessionAPIErrorRetriable(err error) bool {
	var statusErr sessionAPIStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode >= http.StatusInternalServerError
	}
	return true
}

func (r *runner) runLogged(ctx context.Context, dir string, extraEnv []string, logName string, name string, args ...string) error {
	logPath := filepath.Join(artifactsDir, logName)
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	r.failureLogs = append(r.failureLogs, logPath)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), extraEnv...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return cmd.Run()
}

func (r *runner) capture(ctx context.Context, dir string, extraEnv []string, logName string, name string, args ...string) (string, error) {
	logPath := filepath.Join(artifactsDir, logName)
	logFile, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	r.failureLogs = append(r.failureLogs, logPath)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), extraEnv...)
	var stdout strings.Builder
	cmd.Stdout = io.MultiWriter(&stdout, logFile)
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

func (r *runner) gitHasChanges(ctx context.Context) (bool, error) {
	status, err := r.capture(ctx, repoDir, nil, "git-status.log", "git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(status) != "", nil
}

func (r *runner) writeDeveloperPRBody(runID string, profile string, baseBranch string, headBranch string) (string, error) {
	final, err := os.ReadFile(filepath.Join(artifactsDir, "codex-final.md"))
	if err != nil {
		return "", err
	}
	body := fmt.Sprintf("## Matter-codex developer run\n\n- run: `%s`\n- profile: `%s`\n- base: `%s`\n- head: `%s`\n\n## Codex summary\n\n%s\n", runID, profile, baseBranch, headBranch, strings.TrimSpace(string(final)))
	path := filepath.Join(artifactsDir, "pr-body.md")
	return path, os.WriteFile(path, []byte(body), 0o600)
}

func (r *runner) writeReviewBody(runID string, prNumber string, finalFile string) (string, error) {
	final, err := os.ReadFile(filepath.Join(artifactsDir, finalFile))
	if err != nil {
		return "", err
	}
	body := fmt.Sprintf("Matter-codex reviewer run %s for PR #%s.\n\n%s\n", runID, prNumber, strings.TrimSpace(string(final)))
	path := filepath.Join(artifactsDir, "review-body.md")
	return path, os.WriteFile(path, []byte(body), 0o600)
}

func (r *runner) writeReviewFallbackBody(runID string, prNumber string, failedFlag string, finalFile string) (string, error) {
	final, err := os.ReadFile(filepath.Join(artifactsDir, finalFile))
	if err != nil {
		return "", err
	}
	body := fmt.Sprintf("Matter-codex reviewer could not submit %s for this PR, so it is posting the review as a comment.\n\nMatter-codex reviewer run %s for PR #%s.\n\n%s\n", failedFlag, runID, prNumber, strings.TrimSpace(string(final)))
	path := filepath.Join(artifactsDir, "review-body-fallback.md")
	return path, os.WriteFile(path, []byte(body), 0o600)
}

func writeCodexConfig(path string) error {
	allowlist := codexShellEnvironmentAllowlist()
	config := map[string]any{
		"sandbox_mode":             "danger-full-access",
		"approval_policy":          "never",
		"disable_response_storage": false,
		"shell_environment_policy": map[string]any{
			"inherit":                 "all",
			"ignore_default_excludes": true,
			"include_only":            allowlist,
		},
		"mcp_servers": map[string]any{
			"context7": map[string]any{
				"command":             "npx",
				"args":                []string{"-y", "@upstash/context7-mcp"},
				"startup_timeout_sec": 20,
			},
		},
	}
	if overlay := strings.TrimSpace(os.Getenv("MATTERCODEX_CODEX_CONFIG_OVERLAY")); overlay != "" {
		overlayConfig := map[string]any{}
		if _, err := toml.Decode(overlay, &overlayConfig); err != nil {
			return fmt.Errorf("parse codex config overlay: %w", err)
		}
		mergeTOMLMaps(config, overlayConfig)
	}
	if err := ensureCodexRuntimeConfig(config, allowlist); err != nil {
		return err
	}
	var body bytes.Buffer
	encoder := toml.NewEncoder(&body)
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("encode codex config: %w", err)
	}
	return os.WriteFile(path, body.Bytes(), 0o600)
}

func disableCodexConfigOverlayForAuthCheck() error {
	return os.Setenv("MATTERCODEX_CODEX_CONFIG_OVERLAY", "")
}

func mergeTOMLMaps(dst map[string]any, src map[string]any) {
	for key, srcValue := range src {
		srcMap, srcIsMap := asStringAnyMap(srcValue)
		dstMap, dstIsMap := asStringAnyMap(dst[key])
		if srcIsMap && dstIsMap {
			mergeTOMLMaps(dstMap, srcMap)
			continue
		}
		dst[key] = srcValue
	}
}

func ensureCodexRuntimeConfig(config map[string]any, allowlist []string) error {
	shellPolicy, err := ensureTOMLTable(config, "shell_environment_policy")
	if err != nil {
		return err
	}
	shellPolicy["inherit"] = "all"
	shellPolicy["ignore_default_excludes"] = true
	existingAllowlist, err := stringListValue(shellPolicy["include_only"])
	if err != nil {
		return fmt.Errorf("shell_environment_policy.include_only: %w", err)
	}
	shellPolicy["include_only"] = mergeStringLists(existingAllowlist, allowlist)

	if mcpURL := strings.TrimSpace(os.Getenv("MATTERCODEX_MCP_URL")); mcpURL != "" {
		mcpServers, err := ensureTOMLTable(config, "mcp_servers")
		if err != nil {
			return err
		}
		mcpServers["mattercodex"] = map[string]any{
			"url":                  mcpURL,
			"bearer_token_env_var": "MATTERCODEX_MCP_TOKEN",
			"startup_timeout_sec":  10,
			"tool_timeout_sec":     60,
			"required":             true,
		}
	}
	return nil
}

func ensureTOMLTable(parent map[string]any, key string) (map[string]any, error) {
	if value, exists := parent[key]; exists {
		table, ok := asStringAnyMap(value)
		if !ok {
			return nil, fmt.Errorf("%s must be a TOML table", key)
		}
		return table, nil
	}
	table := map[string]any{}
	parent[key] = table
	return table, nil
}

func asStringAnyMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func stringListValue(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			values = append(values, text)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("must be a string array")
	}
}

func mergeStringLists(base []string, extra []string) []string {
	values := make([]string, 0, len(base)+len(extra))
	seen := map[string]struct{}{}
	for _, value := range append(base, extra...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func codexShellEnvironmentAllowlist() []string {
	values := []string{
		"PATH",
		"HOME",
		"CODEX_HOME",
		"GH_TOKEN",
		"GITHUB_TOKEN",
		"GITHUB_USERNAME",
		"GITHUB_USER",
		"GITHUB_EMAIL",
		"GIT_AUTHOR_NAME",
		"GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME",
		"GIT_COMMITTER_EMAIL",
		"GIT_ASKPASS",
		"MATTERCODEX_GIT_ASKPASS",
		"GIT_TERMINAL_PROMPT",
		"MATTERCODEX_GITHUB_TOKEN_FILE",
		"KUBECONFIG",
		"KUBERNETES_SERVICE_HOST",
		"KUBERNETES_SERVICE_PORT",
		"KUBERNETES_PORT",
		"KUBERNETES_PORT_443_TCP",
		"KUBERNETES_PORT_443_TCP_ADDR",
		"KUBERNETES_PORT_443_TCP_PORT",
		"KUBERNETES_PORT_443_TCP_PROTO",
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, raw := range strings.Split(os.Getenv("MATTERCODEX_RUNTIME_ENV_ALLOWLIST"), ",") {
		name := strings.TrimSpace(raw)
		if name == "" || !validRuntimeEnvName(name) {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		values = append(values, name)
	}
	return values
}

func validRuntimeEnvName(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, char := range value[1:] {
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '_' {
			continue
		}
		return false
	}
	return true
}

func codexSandboxMode() string {
	mode := strings.TrimSpace(os.Getenv("MATTERCODEX_CODEX_SANDBOX_MODE"))
	if mode == "" {
		return "danger-full-access"
	}
	return mode
}

func runGitAskPass() {
	prompt := strings.Join(os.Args[1:], " ")
	switch {
	case strings.Contains(prompt, "Username"):
		fmt.Println("x-access-token")
	case strings.Contains(prompt, "Password"):
		token, err := os.ReadFile(defaultString(os.Getenv("MATTERCODEX_GITHUB_TOKEN_FILE"), gitHubTokenPath))
		if err == nil {
			fmt.Print(strings.TrimSpace(string(token)))
		}
	default:
		fmt.Println()
	}
}

func authReadyCheck() error {
	for _, path := range []string{filepath.Join(codexAuthDir, "auth.json"), filepath.Join(codexAuthDir, ".auth-ready")} {
		if err := requireNonEmptyFile(path); err != nil {
			return err
		}
	}
	return nil
}

func authJSONCheck() error {
	return requireNonEmptyFile(filepath.Join(codexAuthDir, "auth.json"))
}

func requireNonEmptyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("%s is empty", path)
	}
	return nil
}

func printAuthJSON() error {
	if err := authReadyCheck(); err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(codexAuthDir, "auth.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(os.Stdout, file)
	return err
}

func readGitHubAccount() (githubAccount, error) {
	token, err := readRequiredSecretFile(gitHubTokenPath, "github token")
	if err != nil {
		return githubAccount{}, err
	}
	username, err := readRequiredSecretFile(gitHubUserPath, "github username")
	if err != nil {
		return githubAccount{}, err
	}
	email, err := readRequiredSecretFile(gitHubEmailPath, "github email")
	if err != nil {
		return githubAccount{}, err
	}
	return githubAccount{Token: token, Username: username, Email: email}, nil
}

func readRequiredSecretFile(path string, label string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	return value, nil
}

func (account githubAccount) env() []string {
	return []string{
		"GH_TOKEN=" + account.Token,
		"GITHUB_TOKEN=" + account.Token,
		"GITHUB_USERNAME=" + account.Username,
		"GITHUB_USER=" + account.Username,
		"GITHUB_EMAIL=" + account.Email,
		"GIT_AUTHOR_NAME=" + account.Username,
		"GIT_AUTHOR_EMAIL=" + account.Email,
		"GIT_COMMITTER_NAME=" + account.Username,
		"GIT_COMMITTER_EMAIL=" + account.Email,
		"GIT_ASKPASS=" + runnerBinaryPath,
		"MATTERCODEX_GIT_ASKPASS=1",
		"MATTERCODEX_GITHUB_TOKEN_FILE=" + gitHubTokenPath,
		"GIT_TERMINAL_PROMPT=0",
	}
}

func mergeEnv(base []string, overrides ...string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for _, item := range overrides {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func readDecision(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "decision:") {
			_, value, _ := strings.Cut(line, ":")
			return value
		}
	}
	return ""
}

func readReviewSubmitted(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "review_submitted:") {
			_, value, _ := strings.Cut(line, ":")
			return strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	return false
}

func normalizeDecision(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
	switch value {
	case "approve", "approved":
		return "approve"
	case "request_changes", "changes_requested", "requestchanges":
		return "request_changes"
	default:
		return "comment"
	}
}

func reviewFlag(decision string) string {
	switch decision {
	case "approve":
		return "--approve"
	case "request_changes":
		return "--request-changes"
	default:
		return "--comment"
	}
}

func artifact(key string, value string) {
	value = strings.TrimSpace(value)
	if key != "" && value != "" {
		fmt.Printf("matter-codex artifact %s: %s\n", key, value)
	}
}

func (r *runner) printFinalAnswer(finalFile string) {
	body, err := os.ReadFile(filepath.Join(artifactsDir, finalFile))
	if err != nil {
		return
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return
	}
	fmt.Println("matter-codex final answer begin")
	fmt.Println(text)
	fmt.Println("matter-codex final answer end")
}

func fail(err error, logs []string) {
	fmt.Fprintf(os.Stderr, "matter-codex runner error: %v\n", err)
	artifact("exit-code", "1")
	for _, path := range logs {
		tailFile(path, 40)
	}
	os.Exit(1)
}

func tailFile(path string, lines int) {
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	parts := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	fmt.Printf("===== %s\n", path)
	fmt.Println(strings.Join(parts, "\n"))
}

func tailText(path string, lines int) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func readTextFile(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func readCodexSessionID(eventsPath string) string {
	file, err := os.Open(eventsPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var sessionID string
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Type == "thread.started" && strings.TrimSpace(event.ThreadID) != "" {
			sessionID = strings.TrimSpace(event.ThreadID)
		}
	}
	return sessionID
}

func createCodexSessionArchive(root string) (string, error) {
	sessionsRoot := filepath.Join(root, "sessions")
	info, err := os.Stat(sessionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", sessionsRoot)
	}
	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)
	err = filepath.WalkDir(sessionsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(tarWriter, file)
		return err
	})
	if err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return "", err
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return "", err
	}
	if err := gzipWriter.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw.Bytes()), nil
}

func restoreCodexSessionArchive(encoded string, root string) error {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode codex session archive: %w", err)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("open codex session archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read codex session archive: %w", err)
		}
		target, err := safeArchiveTarget(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
}

func safeArchiveTarget(root string, name string) (string, error) {
	name = filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(name) || name == "." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || name == ".." {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	if name != "sessions" && !strings.HasPrefix(name, "sessions"+string(filepath.Separator)) {
		return "", fmt.Errorf("unexpected archive path %q", name)
	}
	return filepath.Join(root, name), nil
}

func copyFile(source string, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(fmt.Sprintf("%s is required", name))
	}
	return value
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
