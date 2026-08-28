import { execFile } from "node:child_process";
import { constants as fsConstants } from "node:fs";
import { open } from "node:fs/promises";
import { isAbsolute } from "node:path";
import { promisify } from "node:util";

import { type Page } from "@playwright/test";

import { loadE2EEnvironment } from "./environment";
import { expect, test } from "./fixtures";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EEnvironment();
const execFileAsync = promisify(execFile);
const githubOwner = "codex-k8s";
const githubRepository = "kodex-integration-e2e";
const terminalStates = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

interface VersionedRef {
  readonly ref: string;
  readonly version: number;
}

interface Project extends VersionedRef {
  readonly name: string;
}

interface Agent extends VersionedRef {
  readonly state: string;
}

interface Connection extends VersionedRef {
  readonly state: string;
  readonly credentialsConfigured: boolean;
}

interface Run {
  readonly ref: string;
  readonly state: string;
  readonly gateRefs: readonly string[];
  readonly safeErrorCode?: string;
}

interface RunWorkspace {
  readonly run: Run;
}

interface OwnerGate extends VersionedRef {
  readonly runRef: string;
  readonly state: string;
}

interface RunEventPage {
  readonly items: readonly {
    readonly messageKind?: string;
    readonly toolCall?: {
      readonly tool: string;
      readonly state: string;
      readonly auditRef: string;
    };
  }[];
}

interface SyntheticDiagnostic {
  readonly journal: string;
  readonly count: number;
  readonly value: string;
  readonly last_effect_key: string;
  readonly replay_count: number;
  readonly last_replay_effect_key: string;
}

interface GitHubRepository {
  readonly full_name: string;
  readonly private: boolean;
}

interface GitHubIssue {
  readonly number: number;
  readonly title: string;
  readonly body: string | null;
  readonly state: string;
  readonly pull_request?: unknown;
}

test.describe("deployed local integration path", () => {
  test.describe.configure({ mode: "serial" });

  test("synthetic READ/WRITE, Human Gate и exact retry", async ({ page }) => {
    const journal = `${environment.resourcePrefix}-journal`;
    const replayValue = `kodex-e2e-replay:${environment.resourcePrefix}`;
    const readbackURL = syntheticReadbackURL();

    await gotoWithRetry(page, "/projects");
    const initial = await readSyntheticDiagnostic(readbackURL, journal);
    expect(initial).toMatchObject({ journal, count: 0, replay_count: 0 });

    const project = await mutateAPI<Project>(page, {
      method: "POST",
      path: "/api/v1/projects",
      body: {
        name: `${environment.resourcePrefix} — integration E2E`,
        purpose: "Одноразовая проверка deployed integration lifecycle.",
        language: "ru",
      },
      expectedStatus: 201,
    });
    const agent = await mutateAPI<Agent>(page, {
      method: "POST",
      path: `/api/v1/projects/${encodeURIComponent(project.ref)}/agents`,
      body: {
        name: `${environment.resourcePrefix} — integration executor`,
        purpose: "Выполнять только один явно заданный typed integration call.",
        roleDescription:
          "Детерминированный исполнитель локального E2E интеграций.",
        initialInstructions: [
          "В каждом задании вызови ровно один MCP-инструмент invoke_integration.",
          "Передай connection_ref, capability_key и input без изменений из задания.",
          "Не используй shell, curl, gh, прямой HTTP или другие инструменты.",
          "После завершения вызова кратко сообщи его безопасный результат и закончи работу.",
        ].join(" "),
      },
      expectedStatus: 201,
    });
    expect(agent.state).toBe("READY");

    let connection = await createAndTestConnection(page, {
      definitionKey: "synthetic",
      name: `${environment.resourcePrefix} — synthetic`,
      publicConfiguration: { journal },
    });
    connection = await changeGrant(
      page,
      connection,
      agent.ref,
      "synthetic.journal.read",
    );
    connection = await changeGrant(
      page,
      connection,
      agent.ref,
      "synthetic.journal.write",
    );

    const readRun = await createRun(
      page,
      project.ref,
      agent.ref,
      "synthetic READ",
      {
        connection_ref: connection.ref,
        capability_key: "synthetic.journal.read",
        input: {},
      },
    );
    await waitForTerminalRun(page, readRun.ref, "SUCCEEDED");
    await expectIntegrationToolCall(page, readRun.ref, "SUCCEEDED");
    expect(await readSyntheticDiagnostic(readbackURL, journal)).toMatchObject({
      count: 0,
      replay_count: 0,
    });

    const rejectedRun = await createRun(
      page,
      project.ref,
      agent.ref,
      "synthetic WRITE reject",
      {
        connection_ref: connection.ref,
        capability_key: "synthetic.journal.write",
        input: { value: `${environment.resourcePrefix}-rejected` },
      },
    );
    const rejectedGate = await waitForOpenGate(page, rejectedRun.ref);
    expect(await readSyntheticDiagnostic(readbackURL, journal)).toMatchObject({
      count: 0,
    });
    const rejected = await resolveGate(page, rejectedGate, "REJECT");
    expect(rejected.state).toBe("REJECTED");
    await waitForTerminalRun(page, rejectedRun.ref, "SUCCEEDED");
    await expectIntegrationToolCall(page, rejectedRun.ref);
    expect(await readSyntheticDiagnostic(readbackURL, journal)).toMatchObject({
      count: 0,
      replay_count: 0,
    });

    const approvedRun = await createRun(
      page,
      project.ref,
      agent.ref,
      "synthetic WRITE replay",
      {
        connection_ref: connection.ref,
        capability_key: "synthetic.journal.write",
        input: { value: replayValue },
      },
    );
    const approvedGate = await waitForOpenGate(page, approvedRun.ref);
    const approved = await resolveGate(page, approvedGate, "APPROVE");
    expect(approved.state).toBe("APPROVED");

    await expect
      .poll(
        async () => (await readSyntheticDiagnostic(readbackURL, journal)).count,
        {
          timeout: 60_000,
          intervals: [50, 100, 200],
        },
      )
      .toBe(1);
    await restartIntegrationGateway();
    await expect
      .poll(
        async () =>
          (await readSyntheticDiagnostic(readbackURL, journal)).replay_count,
        {
          timeout: 120_000,
          intervals: [250, 1_000, 2_000],
        },
      )
      .toBeGreaterThanOrEqual(1);
    await waitForTerminalRun(page, approvedRun.ref, "SUCCEEDED");
    await expectIntegrationToolCall(page, approvedRun.ref, "SUCCEEDED");
    const final = await readSyntheticDiagnostic(readbackURL, journal);
    expect(final).toMatchObject({ count: 1, value: replayValue });
    expect(final.last_effect_key).not.toBe("");
    expect(final.last_replay_effect_key).toBe(final.last_effect_key);
  });

  test("опциональный GitHub READ и обратимый WRITE проходят через MCP", async ({
    page,
  }) => {
    test.skip(
      !githubEnabled(),
      "GitHub fixture требует явного KODEX_INTEGRATION_E2E_GITHUB=1",
    );
    let token: string | undefined = await readGitHubToken();
    const marker = `kodex-integration-e2e:${environment.resourcePrefix}`;
    let cleanupIssueNumber = 0;

    try {
      const repository = await githubRequest<GitHubRepository>(
        token,
        `/repos/${githubOwner}/${githubRepository}`,
      );
      expect(repository).toMatchObject({
        full_name: `${githubOwner}/${githubRepository}`,
        private: true,
      });
      expect(await findGitHubMarkerIssues(token, marker)).toHaveLength(0);

      await gotoWithRetry(page, "/projects");
      const project = await mutateAPI<Project>(page, {
        method: "POST",
        path: "/api/v1/projects",
        body: {
          name: `${environment.resourcePrefix} — GitHub integration E2E`,
          purpose: "Одноразовая проверка private GitHub fixture.",
          language: "ru",
        },
        expectedStatus: 201,
      });
      const agent = await mutateAPI<Agent>(page, {
        method: "POST",
        path: `/api/v1/projects/${encodeURIComponent(project.ref)}/agents`,
        body: {
          name: `${environment.resourcePrefix} — GitHub executor`,
          purpose:
            "Выполнять только явно заданный typed GitHub integration call.",
          roleDescription: "Детерминированный исполнитель GitHub fixture.",
          initialInstructions: [
            "В каждом задании вызови ровно один MCP-инструмент invoke_integration.",
            "Передай connection_ref, capability_key и input без изменений из задания.",
            "Не используй shell, curl, gh, прямой HTTP или другие инструменты.",
            "После вызова кратко сообщи безопасный результат и закончи работу.",
          ].join(" "),
        },
        expectedStatus: 201,
      });

      let connection = await mutateAPI<Connection>(page, {
        method: "POST",
        path: "/api/v1/integration-connections",
        body: {
          definitionKey: "github",
          name: `${environment.resourcePrefix} — private GitHub fixture`,
          publicConfiguration: {
            owner: githubOwner,
            repository: githubRepository,
          },
        },
        expectedStatus: 201,
      });
      connection = await mutateAPI<Connection>(page, {
        method: "POST",
        path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}/credential`,
        body: { value: token },
        version: connection.version,
        expectedStatus: 200,
      });
      expect(connection.credentialsConfigured).toBe(true);
      connection = await testConnection(page, connection);
      connection = await changeGrant(
        page,
        connection,
        agent.ref,
        "github.repository.metadata.read",
      );
      connection = await changeGrant(
        page,
        connection,
        agent.ref,
        "github.issue.create",
      );

      const readRun = await createRun(
        page,
        project.ref,
        agent.ref,
        "GitHub metadata READ",
        {
          connection_ref: connection.ref,
          capability_key: "github.repository.metadata.read",
          input: {},
        },
      );
      await waitForTerminalRun(page, readRun.ref, "SUCCEEDED");
      await expectIntegrationToolCall(page, readRun.ref, "SUCCEEDED");

      const title = `[Kodex E2E] ${environment.resourcePrefix}`;
      const writeRun = await createRun(
        page,
        project.ref,
        agent.ref,
        "GitHub issue WRITE",
        {
          connection_ref: connection.ref,
          capability_key: "github.issue.create",
          input: {
            title,
            body: `Disposable local integration fixture.\n\n<!-- ${marker} -->`,
          },
        },
      );
      const gate = await waitForOpenGate(page, writeRun.ref);
      expect(await findGitHubMarkerIssues(token, marker)).toHaveLength(0);
      expect((await resolveGate(page, gate, "APPROVE")).state).toBe("APPROVED");
      await waitForTerminalRun(page, writeRun.ref, "SUCCEEDED");
      await expectIntegrationToolCall(page, writeRun.ref, "SUCCEEDED");

      await expect
        .poll(
          async () =>
            (await findGitHubMarkerIssues(token ?? "", marker)).length,
          {
            timeout: 60_000,
            intervals: [500, 1_000, 2_000],
          },
        )
        .toBe(1);
      const [issue] = await findGitHubMarkerIssues(token, marker);
      expect(issue).toMatchObject({ title, state: "open" });
      if (!issue) throw new Error("GitHub issue readback is unavailable");
      cleanupIssueNumber = issue.number;
    } finally {
      if (token) {
        const issues =
          cleanupIssueNumber > 0
            ? [{ number: cleanupIssueNumber }]
            : await findGitHubMarkerIssues(token, marker).catch(() => []);
        for (const issue of issues) {
          await githubRequest<GitHubIssue>(
            token,
            `/repos/${githubOwner}/${githubRepository}/issues/${String(issue.number)}`,
            {
              method: "PATCH",
              body: { state: "closed", state_reason: "not_planned" },
            },
          );
        }
      }
      token = undefined;
    }
  });
});

async function createAndTestConnection(
  page: Page,
  input: {
    definitionKey: string;
    name: string;
    publicConfiguration: Record<string, string>;
  },
): Promise<Connection> {
  const connection = await mutateAPI<Connection>(page, {
    method: "POST",
    path: "/api/v1/integration-connections",
    body: input,
    expectedStatus: 201,
  });
  return testConnection(page, connection);
}

async function testConnection(
  page: Page,
  connection: Connection,
): Promise<Connection> {
  await mutateAPI<Connection>(page, {
    method: "POST",
    path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}/commands`,
    body: { action: "TEST" },
    version: connection.version,
    expectedStatus: 200,
  });
  let current = connection;
  await expect
    .poll(
      async () => {
        current = await readAPI<Connection>(
          page,
          `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}`,
        );
        return current.state;
      },
      { timeout: 120_000, intervals: [250, 1_000, 2_000] },
    )
    .toBe("CONNECTED");
  return current;
}

async function changeGrant(
  page: Page,
  connection: Connection,
  agentRef: string,
  capabilityKey: string,
): Promise<Connection> {
  return mutateAPI<Connection>(page, {
    method: "POST",
    path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}/grants`,
    body: { capabilityKey, agentRef, enabled: true },
    version: connection.version,
    expectedStatus: 200,
  });
}

async function createRun(
  page: Page,
  projectRef: string,
  agentRef: string,
  title: string,
  invocation: {
    connection_ref: string;
    capability_key: string;
    input: Record<string, unknown>;
  },
): Promise<Run> {
  const workspace = await mutateAPI<RunWorkspace>(page, {
    method: "POST",
    path: "/api/v1/runs",
    body: {
      projectRef,
      targetRef: agentRef,
      targetType: "AGENT",
      title: `${environment.resourcePrefix} — ${title}`,
      task: [
        "Вызови ровно один MCP-инструмент invoke_integration с этим JSON:",
        JSON.stringify(invocation),
        "Не меняй значения, не вызывай другие инструменты и после результата заверши ответ.",
      ].join("\n"),
    },
    expectedStatus: 201,
  });
  return workspace.run;
}

async function waitForOpenGate(page: Page, runRef: string): Promise<OwnerGate> {
  let gate: OwnerGate | undefined;
  await expect
    .poll(
      async () => {
        const run = await readAPI<Run>(
          page,
          `/api/v1/runs/${encodeURIComponent(runRef)}`,
        );
        for (const gateRef of run.gateRefs) {
          const candidate = await readAPI<OwnerGate>(
            page,
            `/api/v1/owner-gates/${encodeURIComponent(gateRef)}`,
          );
          if (candidate.runRef === runRef && candidate.state === "OPEN") {
            gate = candidate;
            return true;
          }
        }
        return false;
      },
      { timeout: 180_000, intervals: [250, 1_000, 2_000] },
    )
    .toBe(true);
  if (!gate) throw new Error("Open integration gate was not found");
  return gate;
}

async function resolveGate(
  page: Page,
  gate: OwnerGate,
  decision: "APPROVE" | "REJECT",
): Promise<OwnerGate> {
  const receipt = await mutateAPI<{ gate: OwnerGate }>(page, {
    method: "POST",
    path: `/api/v1/owner-gates/${encodeURIComponent(gate.ref)}/resolution`,
    body: { decision, comment: `Local integration E2E: ${decision}` },
    version: gate.version,
    expectedStatus: 200,
  });
  return receipt.gate;
}

async function waitForTerminalRun(
  page: Page,
  runRef: string,
  expectedState: string,
): Promise<Run> {
  let current: Run | undefined;
  await expect
    .poll(
      async () => {
        current = await readAPI<Run>(
          page,
          `/api/v1/runs/${encodeURIComponent(runRef)}`,
        );
        return terminalStates.has(current.state) ? current.state : "PENDING";
      },
      { timeout: environment.runTimeoutMs, intervals: [500, 1_000, 2_000] },
    )
    .toBe(expectedState);
  if (!current) throw new Error("Run terminal readback is unavailable");
  return current;
}

async function expectIntegrationToolCall(
  page: Page,
  runRef: string,
  expectedState?: string,
): Promise<void> {
  const events = await readAPI<RunEventPage>(
    page,
    `/api/v1/runs/${encodeURIComponent(runRef)}/events?afterSequence=0&limit=500`,
  );
  const calls = events.items.filter(
    (event) =>
      event.messageKind === "TOOL_CALL" &&
      event.toolCall?.tool === "invoke_integration",
  );
  expect(calls).toHaveLength(1);
  expect(calls[0]?.toolCall?.auditRef).not.toBe("");
  if (expectedState) expect(calls[0]?.toolCall?.state).toBe(expectedState);
}

async function readAPI<T>(page: Page, path: string): Promise<T> {
  const result = await page.evaluate(async (requestPath) => {
    const response = await fetch(requestPath, {
      headers: { Accept: "application/json" },
    });
    if (!response.ok) {
      let code = "UNKNOWN";
      try {
        code = ((await response.json()) as { code?: string }).code ?? code;
      } catch {
        // Safe code remains UNKNOWN; provider or secret bodies are never returned.
      }
      return { status: response.status, code, body: undefined };
    }
    const body = (await response.json()) as unknown;
    return { status: response.status, code: "", body };
  }, path);
  if (result.status !== 200 || result.body === undefined) {
    throw new Error(
      `API read failed: status=${String(result.status)} code=${result.code}`,
    );
  }
  return result.body as T;
}

async function mutateAPI<T>(
  page: Page,
  request: {
    method: "POST" | "PATCH" | "PUT" | "DELETE";
    path: string;
    body?: unknown;
    version?: number;
    expectedStatus: number;
  },
): Promise<T> {
  const result = await page.evaluate(async (input) => {
    const prefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
    const csrf = document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(prefix))
      ?.slice(prefix.length);
    if (!csrf) return { status: 0, code: "CSRF_UNAVAILABLE", body: undefined };
    const headers: Record<string, string> = {
      Accept: "application/json",
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
      "X-CSRF-Token": decodeURIComponent(csrf),
    };
    if (input.version !== undefined)
      headers["If-Match"] = `"${String(input.version)}"`;
    const response = await fetch(input.path, {
      method: input.method,
      headers,
      body: input.body === undefined ? undefined : JSON.stringify(input.body),
    });
    if (!response.ok) {
      let code = "UNKNOWN";
      try {
        code = ((await response.json()) as { code?: string }).code ?? code;
      } catch {
        // Do not surface response bodies from a credential mutation.
      }
      return { status: response.status, code, body: undefined };
    }
    const body = (await response.json()) as unknown;
    return { status: response.status, code: "", body };
  }, request);
  if (result.status !== request.expectedStatus || result.body === undefined) {
    throw new Error(
      `API mutation failed: status=${String(result.status)} code=${result.code}`,
    );
  }
  return result.body as T;
}

function syntheticReadbackURL(): URL {
  const raw = process.env.KODEX_E2E_SYNTHETIC_READBACK_URL ?? "";
  const parsed = new URL(raw);
  if (
    parsed.protocol !== "http:" ||
    parsed.hostname !== "127.0.0.1" ||
    !parsed.port ||
    parsed.username ||
    parsed.password ||
    parsed.pathname !== "/" ||
    parsed.search ||
    parsed.hash
  ) {
    throw new Error(
      "KODEX_E2E_SYNTHETIC_READBACK_URL must be an exact loopback HTTP origin",
    );
  }
  return parsed;
}

async function readSyntheticDiagnostic(
  base: URL,
  journal: string,
): Promise<SyntheticDiagnostic> {
  const endpoint = new URL(
    `/v1/diagnostics/journals/${encodeURIComponent(journal)}`,
    base,
  );
  const response = await fetch(endpoint, {
    headers: { Accept: "application/json" },
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok)
    throw new Error(
      `Synthetic readback failed with status ${String(response.status)}`,
    );
  return (await response.json()) as SyntheticDiagnostic;
}

async function restartIntegrationGateway(): Promise<void> {
  const childEnvironment = { ...process.env };
  delete childEnvironment.KODEX_GITHUB_BOT_PAT;
  delete childEnvironment.KODEX_GITHUB_BOT_PAT_FILE;
  try {
    await execFileAsync(
      "kubectl",
      [
        "-n",
        "kodex-system",
        "delete",
        "pod",
        "-l",
        "app.kubernetes.io/name=integration-gateway,app.kubernetes.io/component=integration-worker",
        "--grace-period=0",
        "--force",
        "--wait=false",
      ],
      { env: childEnvironment, timeout: 30_000, maxBuffer: 64 << 10 },
    );
    await execFileAsync(
      "kubectl",
      [
        "-n",
        "kodex-system",
        "rollout",
        "status",
        "deployment/integration-gateway",
        "--timeout=180s",
      ],
      { env: childEnvironment, timeout: 190_000, maxBuffer: 64 << 10 },
    );
  } catch {
    throw new Error("Integration gateway restart failed");
  }
}

function githubEnabled(): boolean {
  const raw = process.env.KODEX_INTEGRATION_E2E_GITHUB;
  if (raw === undefined || raw === "0") return false;
  if (raw !== "1")
    throw new Error("KODEX_INTEGRATION_E2E_GITHUB must be 0 or 1");
  return true;
}

async function readGitHubToken(): Promise<string> {
  const direct = process.env.KODEX_GITHUB_BOT_PAT;
  const file = process.env.KODEX_GITHUB_BOT_PAT_FILE;
  if ((direct === undefined) === (file === undefined)) {
    throw new Error("Exactly one GitHub token source must be configured");
  }
  if (direct !== undefined) return validateToken(direct);
  if (!file || !isAbsolute(file))
    throw new Error("KODEX_GITHUB_BOT_PAT_FILE must be absolute");

  const handle = await open(
    file,
    fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW,
  );
  try {
    const stat = await handle.stat();
    if (
      !stat.isFile() ||
      stat.size < 1 ||
      stat.size > 16_384 ||
      (stat.mode & 0o077) !== 0
    ) {
      throw new Error(
        "GitHub token file must be a bounded owner-private regular file",
      );
    }
    if (typeof process.getuid === "function" && stat.uid !== process.getuid()) {
      throw new Error("GitHub token file owner is invalid");
    }
    return validateToken(await handle.readFile({ encoding: "utf8" }));
  } finally {
    await handle.close();
  }
}

function validateToken(raw: string): string {
  const token = raw.trim();
  if (!token || token.length > 16_384 || /[\r\n\0]/.test(token)) {
    throw new Error("GitHub token value is invalid");
  }
  return token;
}

async function githubRequest<T>(
  token: string,
  path: string,
  options: { method?: "GET" | "PATCH"; body?: unknown } = {},
): Promise<T> {
  const response = await fetch(`https://api.github.com${path}`, {
    method: options.method ?? "GET",
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      "X-GitHub-Api-Version": "2022-11-28",
    },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    redirect: "error",
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok)
    throw new Error(
      `GitHub authoritative readback failed with status ${String(response.status)}`,
    );
  return (await response.json()) as T;
}

async function findGitHubMarkerIssues(
  token: string,
  marker: string,
): Promise<GitHubIssue[]> {
  const found: GitHubIssue[] = [];
  for (let page = 1; page <= 5; page += 1) {
    const issues = await githubRequest<GitHubIssue[]>(
      token,
      `/repos/${githubOwner}/${githubRepository}/issues?state=all&per_page=100&page=${String(page)}`,
    );
    found.push(
      ...issues.filter(
        (issue) =>
          !issue.pull_request &&
          (issue.body ?? "").includes(`<!-- ${marker} -->`),
      ),
    );
    if (issues.length < 100) break;
  }
  return found;
}
