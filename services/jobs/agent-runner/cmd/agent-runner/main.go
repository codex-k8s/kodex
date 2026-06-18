package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

type runner struct {
	failureLogs []string
}

type githubAccount struct {
	Token    string
	Username string
	Email    string
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
	case "print-auth-json":
		err = printAuthJSON()
	case "developer":
		err = r.runDeveloper(ctx)
	case "reviewer":
		err = r.runReviewer(ctx)
	case "chat":
		err = r.runChat(ctx)
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
	body := `sandbox_mode = "danger-full-access"
approval_policy = "never"
disable_response_storage = true

[shell_environment_policy]
inherit = "none"
include_only = ["PATH", "HOME", "GH_TOKEN", "GITHUB_TOKEN", "GITHUB_USERNAME", "GITHUB_USER", "GITHUB_EMAIL", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_ASKPASS", "GIT_TERMINAL_PROMPT", "MATTERCODEX_GITHUB_TOKEN_FILE"]

[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
startup_timeout_sec = 20
`
	if overlay := strings.TrimSpace(os.Getenv("MATTERCODEX_CODEX_CONFIG_OVERLAY")); overlay != "" {
		body += "\n# matter-codex role config overlay\n" + overlay + "\n"
	}
	return os.WriteFile(path, []byte(body), 0o600)
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
