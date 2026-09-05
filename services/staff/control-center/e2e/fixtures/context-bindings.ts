import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  AgentContextBinding,
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentSet,
} from "../../src/shared/api/generated/openapi/types.gen";
export async function installContextBindingFixture(
  page: Page,
  projectRef: string,
  environment: RuntimeEnvironmentSet,
) {
  const now = "2026-09-05T00:00:00Z";
  const digest = "a".repeat(64);
  const agent: Agent = {
    ref: "agent_context",
    version: 17,
    projectRef,
    name: "Контекст synthetic",
    purpose: "Контекст",
    roleDescription: "",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Synthetic",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: now,
    nextActions: [],
  };
  const view: AgentRuntimeConfigurationView = {
    agentVersion: agent.version,
    skillBindings: [],
    memoryBindings: [],
    configuration: {
      ref: "config_context",
      version: 2,
      agentRef: agent.ref,
      runtimeProfileRef: agent.runtimeRef,
      provider: "synthetic",
      model: "synthetic",
      providerPolicy: {
        ref: "provider_policy",
        version: 1,
        mode: "FIXED",
        accountCandidates: [{ accountRef: "account_synthetic", weight: 1 }],
        digest,
        createdAt: now,
      },
      digest,
      createdAt: now,
    },
    publishedOverlay: {
      ref: "overlay_context",
      version: 1,
      revision: 1,
      state: "PUBLISHED",
      content: "",
      digest,
      validationMessages: [],
      createdAt: now,
    },
    environmentBinding: {
      ref: "environment_binding_context",
      version: 1,
      agentRef: agent.ref,
      environmentRef: environment.ref,
      versionRef: environment.currentVersion.ref,
      digest: environment.currentVersion.digest,
    },
    environment,
    safeEffectiveConfig: "",
  };
  let reads = 0;
  let target:
    | { resourceRef: string; revisionRef: string; digest: string }
    | undefined;
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path === "/api/v1/agents") {
      expect(url.searchParams.get("projectRef")).toBe(projectRef);
      await route.fulfill({ json: { items: [agent], nextPageToken: "" } });
      return;
    }
    if (path === `/api/v1/agents/${agent.ref}`) {
      await route.fulfill({ json: agent });
      return;
    }
    if (path === `/api/v1/agents/${agent.ref}/runtime-configuration`) {
      reads += 1;
      await route.fulfill({
        headers: { ETag: `"${String(view.agentVersion)}"` },
        json: view,
      });
      return;
    }
    if (
      target &&
      path.startsWith(`/api/v1/agents/${agent.ref}/`) &&
      path.endsWith(`/${target.resourceRef}`)
    ) {
      const kind = path.includes("skill-bundles")
        ? "skillBindings"
        : "memoryBindings";
      const body = request.postDataJSON() as {
        revisionRef: string;
        expectedBindingVersion: number;
      };
      const old = view[kind].find(
        (binding) => binding.resourceRef === target?.resourceRef,
      );
      expect(request.headers()["if-match"]).toBe(
        `"${String(view.agentVersion)}"`,
      );
      expect(body).toEqual({
        revisionRef: target.revisionRef,
        expectedBindingVersion: old?.version ?? 0,
      });
      const receipt: AgentContextBinding = {
        ref: `binding_${target.resourceRef}`,
        agentRef: agent.ref,
        resourceRef: target.resourceRef,
        revisionRef: target.revisionRef,
        digest: target.digest,
        version: (old?.version ?? 0) + 1,
      };
      view[kind] = request.method() === "DELETE" ? [] : [receipt];
      view.agentVersion += 1;
      agent.version = view.agentVersion;
      await route.fulfill({ json: receipt });
      return;
    }
    await route.fallback();
  });
  return async (
    resourceRef: string,
    revisionRef: string,
    revisionDigest: string,
  ): Promise<void> => {
    target = { resourceRef, revisionRef, digest: revisionDigest };
    const panel = page.locator(".context-binding");
    await panel
      .getByRole("button", { name: "Привязка к ИИ-сотруднику", exact: true })
      .click();
    await page.getByRole("option", { name: /Контекст synthetic/ }).click();
    await expect(
      panel.getByRole("button", { name: "Привязать ревизию", exact: true }),
    ).toBeEnabled();
    const previousReads = reads;
    await panel
      .getByRole("button", { name: "Привязать ревизию", exact: true })
      .click();
    await expect(panel.locator("dd")).toContainText(revisionRef);
    expect(reads).toBeGreaterThan(previousReads);
    page.once("dialog", (dialog) => dialog.accept());
    await panel.getByRole("button", { name: "Отвязать", exact: true }).click();
    await expect(
      panel.getByRole("button", { name: "Отвязать", exact: true }),
    ).toBeDisabled();
    await expect(panel.locator("dd")).toHaveCount(0);
    await expect(
      panel.getByRole("button", { name: "Привязать ревизию", exact: true }),
    ).toBeEnabled();
  };
}
