import { createHash } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import type {
  ConfigurationWriteBack,
  ManagedConfiguration,
} from "../src/shared/api/generated/openapi/types.gen";

const baseContent = "# Сохраняем исходное форматирование\nkey: old\n";
const proposedContent = "# Сохраняем исходное форматирование\nkey: new\n";
const digest = (value: string) =>
  createHash("sha256").update(value).digest("hex");
const approvalLabel =
  "Подтверждаю этот точный план, отдельную ветку и создание PR/MR";
const approveLabel = "Подтвердить и создать PR/MR";

async function install(page: Page, lostPrepare: boolean) {
  let proposal: ConfigurationWriteBack = {
    ref: "proposal_browser",
    configurationRef: "configuration",
    sourceRef: "source_browser",
    connectionRef: "connection_browser",
    version: 1,
    configurationVersion: 8,
    sourceVersion: 4,
    connectionVersion: 5,
    kind: "ROLE_IMAGE",
    repositoryRef: "owner/repository",
    sourceRefName: "main",
    path: "role.yaml",
    baseCommitSha: "a".repeat(40),
    baseContentSha256: digest(baseContent),
    proposedContentSha256: digest(proposedContent),
    approvalDigest: "b".repeat(64),
    contentFormat: "YAML",
    proposalBranch: "kodex/proposal-browser",
    state: "WAITING_APPROVAL",
    createdAt: "2026-09-06T00:00:00Z",
    expiresAt: "2099-09-06T00:00:00Z",
    nextActions: [
      { action: "APPROVE", enabled: true, reason: "NONE" },
      { action: "REJECT", enabled: true, reason: "NONE" },
      { action: "CANCEL", enabled: true, reason: "NONE" },
    ],
  };
  const configuration: ManagedConfiguration = {
    sourceEditable: true,
    ref: "configuration",
    version: 8,
    kind: "ROLE_IMAGE",
    name: "Образ из Git",
    managedBy: "GIT",
    source: proposal.sourceRef,
    sourceRevision: proposal.baseCommitSha,
    updatedAt: proposal.createdAt,
    currentRevision: {
      sourceAvailable: true,
      ref: "revision_browser",
      revision: 2,
      state: "PUBLISHED",
      contentFormat: "YAML",
      content: baseContent,
      digest: digest(baseContent),
      validationDiagnostics: [],
      createdAt: proposal.createdAt,
    },
    gitSource: {
      ref: proposal.sourceRef,
      version: 4,
      generation: 1,
      connectionRef: proposal.connectionRef,
      providerKey: "github",
      repositoryRef: proposal.repositoryRef,
      refName: "main",
      path: "role.yaml",
      state: "READY",
      acceptedCommitSha: proposal.baseCommitSha,
      acceptedContentSha256: proposal.baseContentSha256,
      acceptedRevisionRef: "revision_browser",
      syncedAt: proposal.createdAt,
    },
  };
  const writes: {
    path: string;
    headers: Record<string, string>;
    body: unknown;
  }[] = [];
  const failures: string[] = [];
  let prepared = false;
  let reads = 0;
  page.on("pageerror", (error) => failures.push(error.message));
  await page.context().addCookies([
    {
      name: "__Host-kodex-csrf",
      value: "s".repeat(43),
      domain: "kodex.test",
      path: "/",
      secure: true,
      sameSite: "Strict",
    },
  ]);
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin !== "https://kodex.test") {
      failures.push("Unexpected origin");
      await route.abort();
      return;
    }
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({
        json: {
          revision: "0".repeat(64),
          environment: "synthetic",
          apiBaseUrl: "/",
          realtimeUrl: "/api/v1",
          requestTimeoutMs: 10000,
          oidc: {
            authority: "https://identity.invalid",
            clientId: "synthetic",
            redirectUri: "/auth/callback",
            postLogoutRedirectUri: "/",
            scope: "openid",
          },
        },
      });
      return;
    }
    if (request.method() !== "GET") {
      writes.push({
        path: url.pathname,
        headers: request.headers(),
        body: request.postDataJSON() as unknown,
      });
    }
    if (
      url.pathname === "/api/v1/managed-configurations/configuration/revisions"
    ) {
      await route.fulfill({
        json: {
          configuration,
          items: [configuration.currentRevision],
          total: 1,
        },
      });
      return;
    }
    if (
      url.pathname ===
      "/api/v1/managed-configurations/configuration/git-write-backs"
    ) {
      expect(request.method()).toBe("GET");
      expect(url.searchParams.get("pageSize")).toBe("30");
      await route.fulfill({
        json: { items: prepared ? [proposal] : [], total: prepared ? 1 : 0 },
      });
      return;
    }
    if (
      url.pathname ===
      "/api/v1/role-image-configurations/configuration/git-write-backs"
    ) {
      prepared = true;
      if (lostPrepare) await route.abort("failed");
      else await route.fulfill({ status: 201, json: proposal });
      return;
    }
    if (
      url.pathname ===
      "/api/v1/managed-configuration-git-write-backs/proposal_browser/approve"
    ) {
      proposal = {
        ...proposal,
        version: 2,
        state: "UNKNOWN_OUTCOME",
        failureCode: "OUTCOME_UNCONFIRMED",
        nextActions: [
          { action: "APPROVE", enabled: false, reason: "OUTCOME_UNKNOWN" },
          { action: "REJECT", enabled: false, reason: "OUTCOME_UNKNOWN" },
          { action: "CANCEL", enabled: false, reason: "OUTCOME_UNKNOWN" },
        ],
      };
      await route.fulfill({ json: proposal });
      return;
    }
    if (
      url.pathname ===
      "/api/v1/managed-configuration-git-write-backs/proposal_browser"
    ) {
      reads += 1;
      await route.fulfill({ json: { proposal, baseContent, proposedContent } });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push(`Unexpected API ${request.method()} ${url.pathname}`);
      await route.fulfill({ status: 404, json: {} });
      return;
    }
    const response = await route.fetch({
      url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
    });
    await route.fulfill({ response });
  });
  return {
    writes,
    failures,
    revokeSource: () => {
      configuration.sourceEditable = false;
      if (configuration.currentRevision)
        configuration.currentRevision = {
          ...configuration.currentRevision,
          sourceAvailable: false,
          content: "",
          validationDiagnostics: [],
        };
    },
    reads: () => reads,
    succeed: () => {
      proposal = {
        ...proposal,
        version: 3,
        state: "SUCCEEDED",
        failureCode: undefined,
        candidateCommitSha: "c".repeat(40),
        pullRequestRef: "17",
        pullRequestUrl: "https://github.com/owner/repository/pull/17",
        branchConfirmedAt: proposal.createdAt,
        pullRequestConfirmedAt: proposal.createdAt,
        completedAt: proposal.createdAt,
      };
    },
  };
}

for (const width of [390, 2900]) {
  test(`synthetic: managed RoleImage скрывает ранее прочитанный source после отзыва ${String(width)}px`, async ({
    page,
  }) => {
    const fixture = await install(page, false);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/e2e/fixtures/impact.html?kind=git");
    const editor = page.locator(".configuration-editor");
    await expect(
      editor.getByRole("button", { name: "Изменения", exact: true }),
    ).toBeVisible();
    await expect(
      editor.getByRole("region", { name: "Изменение через Git", exact: true }),
    ).toBeVisible();
    fixture.revokeSource();
    await editor
      .locator(":scope > header")
      .getByRole("button", { name: "Обновить", exact: true })
      .click();
    await expect(
      editor.getByText(
        "Исходник недоступен с текущими правами. Сведения об образе и разрешённые действия доступны отдельно.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(editor.locator(".cm-editor")).toHaveCount(0);
    await expect(
      editor.getByRole("region", { name: "Изменение через Git", exact: true }),
    ).toHaveCount(0);
    await expect(
      editor.getByRole("button", { name: "Изменения", exact: true }),
    ).toHaveCount(0);
    await expect(
      editor.getByRole("button", { name: "Сохранить черновик", exact: true }),
    ).toBeDisabled();
    expect(fixture.writes).toEqual([]);
    expect(fixture.failures).toEqual([]);
  });
  test(`synthetic: write-back diff, отдельное approval и UNKNOWN recovery ${String(width)}px`, async ({
    page,
  }) => {
    const fixture = await install(page, false);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/e2e/fixtures/impact.html?kind=git");
    const panel = page.getByRole("region", {
      name: "Изменение через Git",
      exact: true,
    });
    await panel
      .getByRole("button", { name: "Предлагаемый документ", exact: true })
      .click();
    await panel
      .getByRole("textbox", { name: "Предлагаемый документ", exact: true })
      .fill(proposedContent);
    await panel
      .getByRole("button", { name: "Подготовить план", exact: true })
      .click();
    await expect(
      panel.getByLabel("Точные изменения исходного файла", { exact: true }),
    ).toContainText("key: new");
    await expect(panel.locator(".code-diff")).toContainText("key: old");
    await expect(panel).toContainText("kodex/proposal-browser");
    const approve = panel.getByRole("button", {
      name: approveLabel,
      exact: true,
    });
    await expect(approve).toBeDisabled();
    expect(fixture.writes).toHaveLength(1);
    expect(fixture.writes[0]?.body).toEqual({
      expectedSourceVersion: 4,
      content: proposedContent,
    });
    expect(fixture.writes[0]?.headers["if-match"]).toBe('"8"');
    expect(fixture.writes[0]?.headers["x-csrf-token"]).toBe("s".repeat(43));
    expect(fixture.writes[0]?.headers["idempotency-key"]).toBeTruthy();
    await panel.getByLabel(approvalLabel, { exact: true }).check();
    await approve.click();
    await expect(panel).toContainText("Исход внешнего действия не подтверждён");
    await expect(approve).toBeDisabled();
    await expect(
      panel.getByRole("button", { name: "Отменить", exact: true }),
    ).toBeDisabled();
    expect(fixture.writes).toHaveLength(2);
    expect(fixture.writes[1]?.headers["if-match"]).toBe('"1"');
    expect(fixture.writes[1]?.body).toEqual({ approvalDigest: "b".repeat(64) });
    expect(fixture.writes[1]?.headers["idempotency-key"]).not.toBe(
      fixture.writes[0]?.headers["idempotency-key"],
    );
    const reads = fixture.reads();
    fixture.succeed();
    await expect(
      panel.getByRole("link", { name: "Открыть PR/MR", exact: true }),
    ).toHaveAttribute("href", "https://github.com/owner/repository/pull/17");
    expect(fixture.reads()).toBeGreaterThan(reads);
    expect(fixture.writes).toHaveLength(2);
    expect(fixture.failures).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}

test("synthetic: неизвестный Prepare переживает reload, history adoption не является approval", async ({
  page,
}) => {
  const fixture = await install(page, true);
  await page.goto("/e2e/fixtures/impact.html?kind=git");
  const panel = page.getByRole("region", {
    name: "Изменение через Git",
    exact: true,
  });
  await panel
    .getByRole("button", { name: "Предлагаемый документ", exact: true })
    .click();
  await panel
    .getByRole("textbox", { name: "Предлагаемый документ", exact: true })
    .fill(proposedContent);
  await panel
    .getByRole("button", { name: "Подготовить план", exact: true })
    .click();
  await expect(panel).toContainText("Результат запроса неизвестен");
  expect(fixture.writes).toHaveLength(1);
  const stored = await page.evaluate(() =>
    sessionStorage.getItem("kodex.writeback.intent.configuration"),
  );
  expect(stored).toContain(digest(proposedContent));
  expect(stored).not.toContain("key: new");
  await page.reload();
  await expect(panel).toContainText("Результат запроса неизвестен");
  await panel
    .getByRole("button", {
      name: "Открыть план: proposal_browser",
      exact: true,
    })
    .click();
  const adopt = panel.getByRole("button", {
    name: "Выбрать этот план",
    exact: true,
  });
  await expect(adopt).toBeDisabled();
  await panel
    .getByLabel("Продолжить с этим проверенным планом из истории", {
      exact: true,
    })
    .check();
  await adopt.click();
  await expect(adopt).toBeHidden();
  expect(
    await page.evaluate(() =>
      sessionStorage.getItem("kodex.writeback.intent.configuration"),
    ),
  ).toBeNull();
  expect(fixture.writes).toHaveLength(1);
  await expect(
    panel.getByLabel(approvalLabel, { exact: true }),
  ).not.toBeChecked();
  await expect(
    panel.getByRole("button", { name: approveLabel, exact: true }),
  ).toBeDisabled();
  expect(fixture.failures).toEqual([]);
});
