import { execFile } from "node:child_process";
import { constants as fsConstants } from "node:fs";
import { open } from "node:fs/promises";
import { isAbsolute } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { type Page } from "@playwright/test";

import type {
  AgentRuntimeConfigurationView,
  ModelCapabilityPage,
  ProviderAccountCandidateInput,
  ProviderAccountBlockerPage,
  ProviderAccountDeletion,
} from "../src/shared/api/generated/openapi";

import { loadE2EEnvironment } from "./environment";
import { expect, test } from "./fixtures";
import { gotoWithRetry } from "./helpers";
import {
  providerCleanupQuery,
  providerCleanupProjection,
  providerCleanupTargets,
  verifyProviderCleanupFence,
} from "./provider-cleanup-proof";

const environment = loadE2EEnvironment();
const execFileAsync = promisify(execFile);
const terminalStates = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

function execFileWithInput(
  file: string,
  args: readonly string[],
  input: string,
  options: {
    readonly env: NodeJS.ProcessEnv;
    readonly maxBuffer: number;
    readonly timeout: number;
  },
): Promise<{ readonly stderr: string; readonly stdout: string }> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      file,
      [...args],
      { ...options, encoding: "utf8" },
      (error, stdout, stderr) => {
        if (error) {
          const rejection = new Error(error.message, { cause: error });
          Object.assign(rejection, { stderr, stdout });
          reject(rejection);
          return;
        }
        resolve({ stderr, stdout });
      },
    );
    if (child.stdin === null) {
      child.kill();
      reject(new Error("child process stdin is unavailable"));
      return;
    }
    child.stdin.end(input);
  });
}

interface VersionedRef {
  readonly ref: string;
  readonly version: number;
}

interface Project extends VersionedRef {
  readonly name: string;
}

interface Agent extends VersionedRef {
  readonly name: string;
  readonly state: string;
  readonly nextActions: readonly string[];
}

interface ProviderAccount extends VersionedRef {
  readonly deletion?: ProviderAccountDeletion;
  readonly authorization?: { readonly method: string; readonly state: string };
  readonly enabled: boolean;
  readonly externalAccountMasked: string;
  readonly name: string;
  readonly nextActions: readonly string[];
  readonly ready: boolean;
  readonly state: string;
}

interface Connection extends VersionedRef {
  readonly name: string;
  readonly state: string;
  readonly credentialsConfigured: boolean;
  readonly publicConfiguration: Readonly<Record<string, unknown>>;
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

  test("synthetic READ/WRITE, Human Gate и exact retry", async ({
    browserDiagnostics,
    page,
  }) => {
    const journal = `${environment.resourcePrefix}-journal`;
    const replayValue = `kodex-e2e-replay:${environment.resourcePrefix}`;
    const readbackURL = syntheticReadbackURL();

    await browserDiagnostics.withExpectedNetworkInterruption(page, () =>
      gotoWithRetry(page, "/projects"),
    );
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
    const connectedVersion = connection.version;
    connection = await commandConnection(page, connection, "DISABLE");
    expect(connection.state).toBe("DISABLED");
    await expectMutationError(page, {
      method: "POST",
      path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}/commands`,
      body: { action: "ENABLE" },
      version: connectedVersion,
      expectedStatus: 412,
      expectedCode: "VERSION_OR_STATE_CONFLICT",
    });
    connection = await commandConnection(page, connection, "ENABLE");
    connection = await testConnection(page, connection);
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
    connection = await updateConnection(page, connection, {
      name: `${environment.resourcePrefix} — synthetic updated`,
      publicConfiguration: { journal },
    });
    expect(connection.name).toContain("synthetic updated");
    expect(connection.publicConfiguration).toEqual({ journal });
    connection = await commandConnection(page, connection, "DISABLE");
    expect(connection.state).toBe("DISABLED");
    connection = await deleteConnection(page, connection);
    expect(connection.state).toBe("DELETED");
  });

  test("опциональный GitHub READ и обратимый WRITE проходят через MCP", async ({
    page,
  }) => {
    test.skip(
      !githubEnabled(),
      "GitHub fixture требует явного KODEX_INTEGRATION_E2E_GITHUB=1",
    );
    const github = githubFixtureConfiguration();
    let token: string | undefined = await readGitHubToken();
    const marker = `kodex-integration-e2e:${environment.resourcePrefix}`;
    let cleanupIssueNumber = 0;

    try {
      const repository = await githubRequest<GitHubRepository>(
        token,
        githubRepositoryPath(github),
      );
      expect(repository).toMatchObject({
        full_name: `${github.owner}/${github.repository}`,
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
            owner: github.owner,
            repository: github.repository,
          },
        },
        expectedStatus: 201,
      });
      connection = await mutateAPI<Connection>(page, {
        method: "PUT",
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
            `${githubRepositoryPath(github)}/issues/${String(issue.number)}`,
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

  test("опциональный API key account выполняет run с exact affinity", async ({
    page,
  }) => {
    test.skip(
      !providerAPIKeyEnabled(),
      "API key fixture требует явного KODEX_PROVIDER_E2E_API_KEY=1",
    );
    let apiKey: string | undefined = await readProviderAPIKey();
    let account: ProviderAccount | undefined;
    let fixtureProject: Project | undefined;
    let fixtureAgent: Agent | undefined;
    let accountBound = false;

    try {
      await gotoWithRetry(page, "/administration/providers");
      await page
        .getByRole("button", { name: "Добавить учётную запись" })
        .click();
      const createDialog = page.getByRole("dialog", {
        name: "Добавить учётную запись",
      });
      await createDialog
        .getByLabel("Название")
        .fill(`${environment.resourcePrefix} — API key account`);
      const creationResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname === "/api/v1/provider-accounts",
      );
      await createDialog
        .getByRole("button", { name: "Создать", exact: true })
        .click();
      const created = await creationResponse;
      expect(created.status()).toBe(201);
      account = (await created.json()) as ProviderAccount;
      expect(account).toMatchObject({
        enabled: false,
        state: "PENDING_AUTHORIZATION",
      });
      expect(account.nextActions).toContain("CONFIGURE_CREDENTIAL");
      const accountReadback = await readAPI<ProviderAccount>(
        page,
        `/api/v1/provider-accounts/${encodeURIComponent(account.ref)}`,
      );
      expect(accountReadback.nextActions).toContain("CONFIGURE_CREDENTIAL");

      const authorizationDialog = page.getByRole("dialog", {
        name: new RegExp(`Авторизация: ${escapeRegExp(account.name)}`),
      });
      await authorizationDialog.getByRole("tab", { name: "API key" }).click();
      await authorizationDialog.getByLabel("API key").fill(apiKey);
      const authorizationResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname ===
            `/api/v1/provider-accounts/${account?.ref ?? ""}/api-key-authorization`,
      );
      await authorizationDialog
        .getByRole("button", { name: "Авторизовать", exact: true })
        .click();
      apiKey = undefined;
      const authorized = await authorizationResponse;
      const authorizedPayload = (await authorized.json()) as ProviderAccount;
      expect(authorized.status(), JSON.stringify(authorizedPayload)).toBe(200);
      account = authorizedPayload;
      expect(account).toMatchObject({
        authorization: { method: "API_KEY", state: "AUTHORIZED" },
        enabled: true,
        state: "AUTHORIZED",
      });
      expect(account.externalAccountMasked).not.toBe("");
      await expect(
        authorizationDialog.getByText("Учётная запись авторизована."),
      ).toBeVisible();
      await authorizationDialog
        .locator(".modal__footer")
        .getByRole("button", { name: "Закрыть", exact: true })
        .click();

      const project = await mutateAPI<Project>(page, {
        method: "POST",
        path: "/api/v1/projects",
        body: {
          name: `${environment.resourcePrefix} — API key E2E`,
          purpose: "Одноразовая проверка API key provider affinity.",
          language: "ru",
        },
        expectedStatus: 201,
      });
      fixtureProject = project;
      const agent = await mutateAPI<Agent>(page, {
        method: "POST",
        path: `/api/v1/projects/${encodeURIComponent(project.ref)}/agents`,
        body: {
          name: `${environment.resourcePrefix} — API key executor`,
          purpose: "Подтвердить реальный запуск через API key account.",
          roleDescription: "Детерминированный исполнитель provider E2E.",
          initialInstructions:
            "Отвечай по-русски одним коротким предложением. Не вызывай инструменты.",
        },
        expectedStatus: 201,
      });
      fixtureAgent = agent;
      await pinAgentProviderAccount(page, agent.ref, account.ref);
      accountBound = true;
      const run = await createPlainRun(
        page,
        project.ref,
        agent.ref,
        "Ответь ровно: API key provider path работает.",
      );
      await waitForTerminalRun(page, run.ref, "SUCCEEDED");
      await verifySingleProviderAffinity(run.ref, account.ref);
    } finally {
      apiKey = undefined;
      const credentialInput = page.locator('input[type="password"]');
      if ((await credentialInput.count()) > 0) {
        await credentialInput.fill("").catch(() => undefined);
      }
      if (account) {
        await deleteFixtureProviderAccount(page, account.ref, {
          agent: fixtureAgent,
          project: fixtureProject,
          accountBound,
        });
      }
    }
  });
});

async function deleteFixtureProviderAccount(
  page: Page,
  accountRef: string,
  fixture: {
    agent?: Agent;
    project?: Project;
    accountBound: boolean;
  },
): Promise<void> {
  const accountPath = `/api/v1/provider-accounts/${encodeURIComponent(accountRef)}`;
  let current = await readAPI<ProviderAccount>(page, accountPath);
  if (current.state !== "DELETED" && current.state !== "DELETING") {
    current = await requestFixtureProviderDeletion(page, current);
  }
  expect(["DELETING", "DELETED"]).toContain(current.state);
  if (fixture.agent && current.state !== "DELETED") {
    const query = new URLSearchParams({
      kind: "AGENT",
      query: fixture.agent.name,
      pageSize: "100",
    });
    const blockers = await readAPI<ProviderAccountBlockerPage>(
      page,
      `${accountPath}/blockers?${query}`,
    );
    expect(blockers.hiddenCount).toBe(0);
    const ownBlocker = blockers.items.find(
      (item) => item.ref === fixture.agent?.ref,
    );
    if (fixture.accountBound) {
      expect(ownBlocker).toMatchObject({
        kind: "AGENT",
        projectRef: fixture.project?.ref,
      });
    }
    const agentPath = `/api/v1/agents/${encodeURIComponent(fixture.agent.ref)}`;
    const agent = await readAPI<Agent>(page, agentPath);
    if (agent.state !== "ARCHIVED") {
      expect(agent.nextActions).toContain("ARCHIVE");
      const archived = await mutateAPI<Agent>(page, {
        method: "POST",
        path: `${agentPath}/commands`,
        version: agent.version,
        body: { action: "ARCHIVE" },
        expectedStatus: 200,
      });
      expect(archived.state).toBe("ARCHIVED");
    }
    // Архив снимает только зависимость этого Agent, не чужие warm/active runtimes.
    await expect
      .poll(
        async () => {
          const fresh = await readAPI<ProviderAccount>(page, accountPath);
          if (fresh.state === "DELETED") return true;
          const remaining = await readAPI<ProviderAccountBlockerPage>(
            page,
            `${accountPath}/blockers?${query}`,
          );
          return remaining.total === 0 && remaining.hiddenCount === 0;
        },
        { timeout: 60_000, intervals: [250, 1_000, 2_000] },
      )
      .toBe(true);
  }
  await expect
    .poll(
      async () => {
        current = await readAPI<ProviderAccount>(page, accountPath);
        return (
          current.state === "DELETED" || current.deletion?.state === "FAILED"
        );
      },
      { timeout: 180_000, intervals: [250, 1_000, 2_000, 5_000] },
    )
    .toBe(true);
  if (current.deletion?.state === "FAILED") {
    await test.step("Один явный повтор удаления после authoritative FAILED", async () => {
      current = await readAPI<ProviderAccount>(page, accountPath);
      if (current.state === "DELETED") return;
      expect(current.state).toBe("DELETING");
      expect(current.deletion?.state).toBe("FAILED");
      expect(current.nextActions).toContain("DELETE");
      current = await requestFixtureProviderDeletion(page, current);
    });
  }
  // После единственного нового intent повторный FAILED остаётся FAIL, не новым retry.
  await expect
    .poll(
      async () => {
        current = await readAPI<ProviderAccount>(page, accountPath);
        expect(current.deletion?.state).not.toBe("FAILED");
        return current.state;
      },
      { timeout: 180_000, intervals: [250, 1_000, 2_000, 5_000] },
    )
    .toBe("DELETED");
  expect(current).toMatchObject({
    enabled: false,
    ready: false,
    deletion: { state: "DELETED", pendingCleanup: 0 },
  });
  expect(current.deletion?.blockers).toHaveLength(6);
  expect(current.deletion?.blockers.every((item) => item.total === 0)).toBe(
    true,
  );
  const catalogQuery = new URLSearchParams({
    query: current.name,
    pageSize: "100",
  });
  const cursors = new Set<string>();
  let observed = 0;
  let complete = false;
  for (let index = 0; index < 20; index += 1) {
    const catalog = await readAPI<{
      items: ProviderAccount[];
      total: number;
      nextPageToken?: string;
    }>(page, `/api/v1/provider-accounts?${catalogQuery}`);
    expect(catalog.items.some((item) => item.ref === accountRef)).toBe(false);
    observed += catalog.items.length;
    if (!catalog.nextPageToken) {
      expect(observed).toBe(catalog.total);
      complete = true;
      break;
    }
    expect(cursors.has(catalog.nextPageToken)).toBe(false);
    cursors.add(catalog.nextPageToken);
    catalogQuery.set("pageToken", catalog.nextPageToken);
  }
  expect(complete).toBe(true);
  await expect
    .poll(() => verifyProviderCredentialCleanup(accountRef), {
      timeout: 180_000,
      intervals: [250, 1_000, 2_000, 5_000],
    })
    .toBe(true);
}

async function requestFixtureProviderDeletion(
  page: Page,
  account: ProviderAccount,
): Promise<ProviderAccount> {
  expect(account.nextActions).toContain("DELETE");
  const request = {
    method: "DELETE" as const,
    path: `/api/v1/provider-accounts/${encodeURIComponent(account.ref)}`,
    version: account.version,
    idempotencyKey: await page.evaluate(() => crypto.randomUUID()),
    expectedStatus: 200,
  };
  try {
    return await mutateAPI<ProviderAccount>(page, request);
  } catch (error) {
    if (
      error instanceof APIMutationFailure &&
      error.status >= 400 &&
      error.status < 500
    )
      throw error;
    // Это отдельный ограниченный recovery этап fixture, не транспортный retry.
    return await test.step("Восстановить исходный Delete с прежними key и OCC", async () => {
      const fresh = await readAPI<ProviderAccount>(page, request.path);
      expect(fresh.ref).toBe(account.ref);
      return await mutateAPI<ProviderAccount>(page, request);
    });
  }
}

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
  let current = await mutateAPI<Connection>(page, {
    method: "POST",
    path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}/commands`,
    body: { action: "TEST" },
    version: connection.version,
    expectedStatus: 200,
  });
  await expect
    .poll(
      async () => {
        const observed = await readAPIDuringPoll<Connection>(
          page,
          `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}`,
        );
        if (!observed) return current.state;
        current = observed;
        return current.state;
      },
      { timeout: 120_000, intervals: [250, 1_000, 2_000] },
    )
    .toBe("CONNECTED");
  return current;
}

async function commandConnection(
  page: Page,
  connection: Connection,
  action: "DISABLE" | "ENABLE",
): Promise<Connection> {
  return mutateAPI<Connection>(page, {
    method: "POST",
    path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}/commands`,
    body: { action },
    version: connection.version,
    expectedStatus: 200,
  });
}

async function updateConnection(
  page: Page,
  connection: Connection,
  input: {
    name: string;
    publicConfiguration: Record<string, string>;
  },
): Promise<Connection> {
  return mutateAPI<Connection>(page, {
    method: "PATCH",
    path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}`,
    body: input,
    version: connection.version,
    expectedStatus: 200,
  });
}

async function deleteConnection(
  page: Page,
  connection: Connection,
): Promise<Connection> {
  return mutateAPI<Connection>(page, {
    method: "DELETE",
    path: `/api/v1/integration-connections/${encodeURIComponent(connection.ref)}`,
    version: connection.version,
    expectedStatus: 200,
  });
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

async function createPlainRun(
  page: Page,
  projectRef: string,
  agentRef: string,
  task: string,
): Promise<Run> {
  const workspace = await mutateAPI<RunWorkspace>(page, {
    method: "POST",
    path: "/api/v1/runs",
    body: {
      projectRef,
      targetRef: agentRef,
      targetType: "AGENT",
      title: `${environment.resourcePrefix} — API key provider run`,
      task,
    },
    expectedStatus: 201,
  });
  return workspace.run;
}

async function pinAgentProviderAccount(
  page: Page,
  agentRef: string,
  accountRef: string,
): Promise<void> {
  const runtimePath = `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`;
  const runtime = await readAPI<AgentRuntimeConfigurationView>(
    page,
    runtimePath,
  );
  const query = new URLSearchParams({
    providerAccountRef: accountRef,
    query: runtime.configuration.model,
    pageSize: "100",
  });
  let candidate: ProviderAccountCandidateInput | undefined;
  let catalogPin = "";
  const cursors = new Set<string>();
  for (let index = 0; index < 20; index += 1) {
    const catalog = await readAPI<ModelCapabilityPage>(
      page,
      `/api/v1/model-capabilities?${query}`,
    );
    expect(catalog.catalogStatus?.state).toBe("READY");
    expect(Date.parse(catalog.catalogStatus?.expiresAt ?? "")).toBeGreaterThan(
      Date.now(),
    );
    expect(catalog.catalogRevision).toMatch(/^mcat_[a-f0-9]{64}$/);
    expect(catalog.catalogDigest).toMatch(/^[a-f0-9]{64}$/);
    const pin = `${catalog.catalogRevision}:${catalog.catalogDigest}`;
    if (catalogPin) expect(pin).toBe(catalogPin);
    catalogPin = pin;
    const model = catalog.items.find(
      (item) => item.id === runtime.configuration.model,
    );
    if (model) {
      expect(model.available).toBe(true);
      expect(model.eligibleProviderAccountRefs).toContain(accountRef);
      candidate = {
        accountRef,
        weight: 1,
        catalogRevision: catalog.catalogRevision,
        catalogDigest: catalog.catalogDigest,
        providerDefinitionKey: model.providerDefinitionKey,
      };
      break;
    }
    if (!catalog.nextPageToken) break;
    expect(cursors.has(catalog.nextPageToken)).toBe(false);
    cursors.add(catalog.nextPageToken);
    query.set("pageToken", catalog.nextPageToken);
  }
  if (!candidate)
    throw new Error("Selected model is absent from the exact account catalog");
  const result = await page.evaluate(
    async ({ expectedAccountRef, expectedAgentRef, candidate, runtime }) => {
      const readbackResponse = await fetch(
        `/api/v1/agents/${encodeURIComponent(expectedAgentRef)}/runtime-configuration`,
      );
      if (!readbackResponse.ok) {
        return { status: readbackResponse.status, detail: "runtime readback" };
      }
      const readback =
        (await readbackResponse.json()) as AgentRuntimeConfigurationView;
      if (
        readback.agentVersion !== runtime.agentVersion ||
        readback.configuration.digest !== runtime.configuration.digest
      ) {
        return {
          status: 0,
          detail: "Runtime changed during catalog selection",
        };
      }
      const currentCandidates =
        readback.configuration.providerPolicy.accountCandidates;
      if (
        readback.configuration.providerPolicy.mode === "FIXED" &&
        currentCandidates.length === 1 &&
        currentCandidates[0]?.accountRef === expectedAccountRef &&
        currentCandidates[0].catalogRevision === candidate.catalogRevision &&
        currentCandidates[0].catalogDigest === candidate.catalogDigest &&
        currentCandidates[0].providerDefinitionKey ===
          candidate.providerDefinitionKey
      ) {
        return { status: 200, detail: "" };
      }
      const csrfPrefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
      const csrf = document.cookie
        .split(";")
        .map((part) => part.trim())
        .find((part) => part.startsWith(csrfPrefix))
        ?.slice(csrfPrefix.length);
      if (!csrf) return { status: 0, detail: "CSRF token is unavailable" };
      const publication = await fetch(
        `/api/v1/agents/${encodeURIComponent(expectedAgentRef)}/runtime-configuration`,
        {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": crypto.randomUUID(),
            "If-Match": `"${String(readback.agentVersion)}"`,
            "X-CSRF-Token": decodeURIComponent(csrf),
          },
          body: JSON.stringify({
            runtimeProfileRef: readback.configuration.runtimeProfileRef,
            model: readback.configuration.model,
            providerPolicyMode: "FIXED",
            providerAccounts: [candidate],
          }),
        },
      );
      return {
        status: publication.status,
        detail: publication.ok ? "" : (await publication.text()).slice(0, 512),
      };
    },
    {
      expectedAccountRef: accountRef,
      expectedAgentRef: agentRef,
      candidate,
      runtime,
    },
  );
  expect(result.status, result.detail).toBe(200);
  const saved = await readAPI<AgentRuntimeConfigurationView>(page, runtimePath);
  expect(saved.configuration.providerPolicy.mode).toBe("FIXED");
  expect(saved.configuration.providerPolicy.accountCandidates).toHaveLength(1);
  expect(saved.configuration.providerPolicy.accountCandidates[0]).toMatchObject(
    candidate,
  );
}

async function waitForOpenGate(page: Page, runRef: string): Promise<OwnerGate> {
  let gate: OwnerGate | undefined;
  await expect
    .poll(
      async () => {
        const run = await readAPIDuringPoll<Run>(
          page,
          `/api/v1/runs/${encodeURIComponent(runRef)}`,
        );
        if (!run) return false;
        for (const gateRef of run.gateRefs) {
          const candidate = await readAPIDuringPoll<OwnerGate>(
            page,
            `/api/v1/owner-gates/${encodeURIComponent(gateRef)}`,
          );
          if (!candidate) return false;
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
        const observed = await readAPIDuringPoll<Run>(
          page,
          `/api/v1/runs/${encodeURIComponent(runRef)}`,
        );
        if (!observed) return "PENDING";
        current = observed;
        if (
          terminalStates.has(current.state) &&
          current.state !== expectedState
        ) {
          throw new Error(
            `Run ${runRef} reached ${current.state} instead of ${expectedState}: ${current.safeErrorCode ?? "UNKNOWN"}`,
          );
        }
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

async function readAPIDuringPoll<T>(
  page: Page,
  path: string,
): Promise<T | undefined> {
  try {
    return await readAPI<T>(page, path);
  } catch (error) {
    // Playwright does not retry expect.poll callback exceptions.
    if (String(error).includes("TypeError: Failed to fetch")) return undefined;
    throw error;
  }
}

class APIMutationFailure extends Error {
  constructor(
    readonly status: number,
    code: string,
  ) {
    super(`API mutation failed: status=${String(status)} code=${code}`);
  }
}

async function mutateAPI<T>(
  page: Page,
  request: {
    method: "POST" | "PATCH" | "PUT" | "DELETE";
    path: string;
    body?: unknown;
    version?: number;
    idempotencyKey?: string;
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
      "Idempotency-Key": input.idempotencyKey ?? crypto.randomUUID(),
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
    throw new APIMutationFailure(result.status, result.code);
  }
  return result.body as T;
}

async function expectMutationError(
  page: Page,
  request: {
    method: "POST" | "PATCH" | "PUT" | "DELETE";
    path: string;
    body?: unknown;
    version?: number;
    expectedStatus: number;
    expectedCode: string;
  },
): Promise<void> {
  const result = await page.evaluate(async (input) => {
    const prefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
    const csrf = document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(prefix))
      ?.slice(prefix.length);
    if (!csrf) return { status: 0, code: "CSRF_UNAVAILABLE" };
    const headers: Record<string, string> = {
      Accept: "application/json",
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
      "X-CSRF-Token": decodeURIComponent(csrf),
    };
    if (input.version !== undefined) {
      headers["If-Match"] = `"${String(input.version)}"`;
    }
    const response = await fetch(input.path, {
      method: input.method,
      headers,
      body: input.body === undefined ? undefined : JSON.stringify(input.body),
    });
    let code = "UNKNOWN";
    try {
      code = ((await response.json()) as { code?: string }).code ?? code;
    } catch {
      // Error bodies from credential operations are intentionally ignored.
    }
    return { status: response.status, code };
  }, request);
  expect(result).toEqual({
    status: request.expectedStatus,
    code: request.expectedCode,
  });
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

function providerAPIKeyEnabled(): boolean {
  const raw = process.env.KODEX_PROVIDER_E2E_API_KEY;
  if (raw === undefined || raw === "0") return false;
  if (raw !== "1") throw new Error("KODEX_PROVIDER_E2E_API_KEY must be 0 or 1");
  return true;
}

async function readProviderAPIKey(): Promise<string> {
  const direct = process.env.OPENAI_API_KEY;
  const file = process.env.KODEX_PROVIDER_E2E_API_KEY_FILE;
  if ((direct === undefined) === (file === undefined)) {
    throw new Error("Exactly one provider API key source must be configured");
  }
  if (direct !== undefined) return validateProviderAPIKey(direct);
  if (!file || !isAbsolute(file))
    throw new Error("KODEX_PROVIDER_E2E_API_KEY_FILE must be absolute");
  return validateProviderAPIKey(
    await readPrivateFile(file, "Provider API key"),
  );
}

function validateProviderAPIKey(raw: string): string {
  if (
    raw.length < 8 ||
    raw.length > 16_384 ||
    raw.trim() !== raw ||
    /[\r\n\0]/.test(raw)
  ) {
    throw new Error("Provider API key value is invalid");
  }
  return raw;
}

async function readPrivateFile(path: string, label: string): Promise<string> {
  const handle = await open(
    path,
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
        `${label} file must be a bounded owner-private regular file`,
      );
    }
    if (typeof process.getuid === "function" && stat.uid !== process.getuid()) {
      throw new Error(`${label} file owner is invalid`);
    }
    return await handle.readFile({ encoding: "utf8" });
  } finally {
    await handle.close();
  }
}

async function verifySingleProviderAffinity(
  runRef: string,
  accountRef: string,
): Promise<void> {
  const childEnvironment = { ...process.env };
  delete childEnvironment.OPENAI_API_KEY;
  delete childEnvironment.KODEX_PROVIDER_E2E_API_KEY_FILE;
  delete childEnvironment.KODEX_GITHUB_BOT_PAT;
  delete childEnvironment.KODEX_GITHUB_BOT_PAT_FILE;
  const contextResult = await execFileAsync(
    "kubectl",
    ["config", "current-context"],
    {
      env: childEnvironment,
      timeout: 10_000,
      maxBuffer: 16 << 10,
    },
  );
  const context = contextResult.stdout.trim();
  if (!context || /prod(?:uction)?/i.test(context)) {
    throw new Error("Exact local Kubernetes context is unavailable");
  }
  const verifier = fileURLToPath(
    new URL(
      "../../../../tools/dev/verify-provider-affinity.sh",
      import.meta.url,
    ),
  );
  const args = [
    "--context",
    context,
    "--expect-run",
    `${runRef}=${accountRef}`,
    "--require-distinct-accounts",
    "1",
  ];
  if (process.env.KUBECONFIG) args.push("--kubeconfig", process.env.KUBECONFIG);
  await execFileAsync(verifier, args, {
    env: childEnvironment,
    timeout: 60_000,
    maxBuffer: 64 << 10,
  });
}

async function verifyProviderCredentialCleanup(
  accountRef: string,
): Promise<boolean> {
  const deadline = Date.now() + 60_000;
  if (!/^pacc_[A-Za-z0-9_-]{8,88}$/.test(accountRef)) {
    throw new Error("Provider account ref is invalid for cleanup readback");
  }
  const childEnvironment = localKubernetesEnvironment();
  const contextResult = await execFileAsync(
    "kubectl",
    ["config", "current-context"],
    { env: childEnvironment, timeout: 10_000, maxBuffer: 16 << 10 },
  );
  const context = contextResult.stdout.trim();
  if (!context || /prod(?:uction)?/i.test(context)) {
    throw new Error("Exact local Kubernetes context is unavailable");
  }
  const result = await execFileWithInput(
    "kubectl",
    [
      "--context",
      context,
      "-n",
      "kodex-system",
      "exec",
      "-i",
      "kodex-postgresql-0",
      "--",
      "psql",
      "-X",
      "-qAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      "control_plane",
      "-v",
      `account_ref=${accountRef}`,
      "-f",
      "-",
    ],
    providerCleanupQuery,
    { env: childEnvironment, timeout: 30_000, maxBuffer: 256 << 10 },
  );
  const targets = providerCleanupTargets(result.stdout.trim(), accountRef);
  for (const target of targets) {
    const remaining = deadline - Date.now();
    if (remaining <= 0)
      throw new Error("Provider cleanup proof deadline exceeded");
    // stdout остаётся только в памяти; ошибка kubectl не публикует Secret.data.
    const secret = await execFileAsync(
      "kubectl",
      [
        "--context",
        context,
        "-n",
        "kodex-runtime",
        "get",
        "secret",
        target.name,
        "-o",
        providerCleanupProjection,
      ],
      {
        env: childEnvironment,
        timeout: Math.min(10_000, remaining),
        maxBuffer: 64 << 10,
      },
    ).catch(() => {
      throw new Error("Provider cleanup fence readback failed");
    });
    verifyProviderCleanupFence(secret.stdout, target);
  }
  return true;
}

function localKubernetesEnvironment(): NodeJS.ProcessEnv {
  const childEnvironment = { ...process.env };
  delete childEnvironment.OPENAI_API_KEY;
  delete childEnvironment.KODEX_PROVIDER_E2E_API_KEY_FILE;
  delete childEnvironment.KODEX_GITHUB_BOT_PAT;
  delete childEnvironment.KODEX_GITHUB_BOT_PAT_FILE;
  return childEnvironment;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function githubFixtureConfiguration(): {
  readonly owner: string;
  readonly repository: string;
} {
  const owner = process.env.KODEX_INTEGRATION_E2E_GITHUB_OWNER ?? "";
  const repository = process.env.KODEX_INTEGRATION_E2E_GITHUB_REPOSITORY ?? "";
  const slugPattern = /^[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,98}[A-Za-z0-9])?$/;
  if (!slugPattern.test(owner) || !slugPattern.test(repository)) {
    throw new Error(
      "KODEX_INTEGRATION_E2E_GITHUB_OWNER and KODEX_INTEGRATION_E2E_GITHUB_REPOSITORY must identify the dedicated fixture repository",
    );
  }
  return { owner, repository };
}

function githubRepositoryPath(configuration: {
  readonly owner: string;
  readonly repository: string;
}): string {
  return `/repos/${encodeURIComponent(configuration.owner)}/${encodeURIComponent(configuration.repository)}`;
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
  const github = githubFixtureConfiguration();
  const found: GitHubIssue[] = [];
  for (let page = 1; page <= 5; page += 1) {
    const issues = await githubRequest<GitHubIssue[]>(
      token,
      `${githubRepositoryPath(github)}/issues?state=all&per_page=100&page=${String(page)}`,
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
