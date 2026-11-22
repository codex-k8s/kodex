import { expect, test } from "@playwright/test";
import type {
  OwnerGate,
  RunGraph,
} from "../src/shared/api/generated/openapi/types.gen";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { syntheticCatalogRun } from "./fixtures/catalog-run";

for (const mode of ["submitted", "exec", "protected", "malformed"] as const) {
  test(`synthetic: Kubernetes child Gate ${mode}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({
      width: mode === "submitted" ? 390 : 1440,
      height: 1000,
    });
    await installSyntheticCsrf(page.context(), "k");
    const failures: string[] = [];
    const writes: unknown[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    const gate: OwnerGate = {
      ref: "gate_addressed",
      version: 2,
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      projectRef: "project_navigation",
      runRef: "run_navigation",
      nodeRef: "node_navigation",
      title: "Согласование Kubernetes",
      contextSummary: "Одно действие",
      consequencesSummary: "Исходный план",
      requestedBy: { ref: "user_navigation", displayName: "Инициатор" },
      state: "OPEN",
      allowedDecisions: ["APPROVE", "REJECT", "CANCEL"],
      decisionConsequences: [],
      openedAt: "2026-11-15T00:00:00Z",
      nextActions: ["RESOLVE_GATE"],
      kubernetesChild: {
        planRef: "kpl_synthetic",
        childRef: "kch_synthetic",
        operation: "kubernetes.patch",
        planDigest: "a".repeat(64),
        inputDigest: "b".repeat(64),
        targetRef: "ktg_synthetic",
        targetRevision: 3,
        intendedName: "",
        intendedGenerateName: "",
        resource: {
          group: "apps",
          version: "v1",
          resource: "deployments",
          namespace: "review",
          name: "web",
          uid: "30000000-0000-4000-8000-000000000001",
          resourceVersion: "17",
          subresource: "",
          namespaced: true,
          allNamespaces: false,
        },
        preview: {
          inputDigest: "b".repeat(64),
          options: [
            {
              key: "patchType",
              kind: "STRING",
              valueJson: '"JSON"',
              redacted: false,
            },
          ],
          changes: [
            {
              operation: "replace",
              path: "/spec/replicas",
              from: "",
              valueKind: "NUMBER",
              valueJson: "0",
              redacted: false,
            },
            {
              operation: "replace",
              path: "/spec/template/spec/containers/0/image",
              from: "",
              valueKind: "STRING",
              valueJson: '"example/web:v2"',
              redacted: false,
            },
            {
              operation: "SUBMIT",
              path: "/spec/template/spec/containers/0/env",
              from: "",
              valueKind: "NONE",
              valueJson: "",
              redacted: true,
            },
          ],
          secretMaterialRef: "",
          limitations: ["SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN"],
        },
      },
    };
    gate.decisionConsequences = gate.allowedDecisions.map((decision) => ({
      decision,
      safeSummary: `i18n:KUBERNETES_CHILD_GATE_${decision}`,
      executesExternalEffect: decision === "APPROVE",
      terminalForRun: false,
    }));
    const child = gate.kubernetesChild;
    if (!child) throw new Error("Fixture child missing");
    if (mode === "exec") {
      child.operation = "kubernetes.exec";
      child.resource.group = "";
      child.resource.resource = "pods";
      child.preview.options = [
        {
          key: "command",
          kind: "ARRAY",
          valueJson: '["ls","-la","/work"]',
          redacted: false,
        },
        { key: "tty", kind: "BOOLEAN", valueJson: "false", redacted: false },
      ];
      child.preview.changes = [];
      child.preview.limitations = [];
    }
    if (mode === "protected") {
      child.resource.group = "";
      child.resource.resource = "secrets";
      child.resource.name = "app";
      child.preview.changes = [];
      child.preview.secretMaterialRef = "krs_synthetic";
      child.preview.limitations = ["PROTECTED_MATERIAL_HIDDEN"];
    }
    if (mode === "malformed")
      child.preview.changes[0] = {
        operation: "replace",
        path: "/spec/replicas",
        from: "",
        valueKind: "NUMBER",
        valueJson: '"private-malformed-sentinel"',
        redacted: false,
      };
    let resolved = false;
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (url.pathname === "/api/v1/owner-gates/gate_addressed/resolution") {
        writes.push(route.request().postDataJSON());
        expect(route.request().headers()["x-csrf-token"]).toBe("k".repeat(43));
        expect(route.request().headers()["if-match"]).toBe('"2"');
        resolved = true;
        const run = {
          ...syntheticCatalogRun(0, "project_navigation"),
          ref: gate.runRef,
          rootRunRef: gate.runRef,
          state: "RUNNING",
        };
        const graph: RunGraph = {
          runRef: gate.runRef,
          revision: 2,
          sequence: 2,
          nodes: [],
          edges: [],
        };
        await route.fulfill({
          json: {
            gate: {
              ...gate,
              state: "APPROVED",
              version: 3,
              decision: "APPROVE",
              nextActions: [],
            },
            run,
            graph,
          },
        });
        return;
      }
      if (url.pathname === "/api/v1/projects/project_navigation") {
        await route.fulfill({
          json: {
            ref: "project_navigation",
            version: 1,
            name: "Проект проверки",
            purpose: "Синтетический Gate",
            language: "ru",
            lifecycle: "ACTIVE",
            agentCount: 1,
            workflowCount: 0,
            activeRunCount: 1,
            pendingGateCount: resolved ? 0 : 1,
            integrationState: "READY",
            updatedAt: "2026-11-15T00:00:00Z",
            nextActions: [],
          },
        });
        return;
      }
      if (url.pathname === "/api/v1/owner-gates/gate_addressed") {
        await route.fulfill({ json: gate });
        return;
      }
      if (url.pathname === "/api/v1/owner-gates") {
        await route.fulfill({
          json: {
            items: resolved ? [] : [gate],
            total: resolved ? 0 : 1,
            nextPageToken: "",
          },
        });
        return;
      }
      if (
        ["/api/v1/projects", "/api/v1/runs", "/api/v1/audit-events"].includes(
          url.pathname,
        )
      ) {
        await route.fulfill({
          json: { items: [], total: 0, nextPageToken: "" },
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/gate-navigation.html");
    const panel = page.getByRole("region", {
      name: "Дочернее действие Kubernetes",
      exact: true,
    });
    if (mode === "malformed") {
      await expect(page.locator(".problem-notice")).toBeVisible();
      await expect(page.locator(".decision-options")).toHaveCount(0);
      await expect(page.locator("body")).not.toContainText(
        "private-malformed-sentinel",
      );
    } else {
      await expect(panel).toBeVisible();
      await expect(panel).toContainText("Отправленные изменения");
      await expect(panel).toContainText("это не полный diff");
      if (mode === "submitted") {
        await expect(panel).toContainText("/spec/replicas");
        await expect(panel).toContainText("example/web:v2");
        await expect(panel).toContainText("Значение скрыто");
      }
      if (mode === "exec")
        await expect(panel).toContainText('["ls","-la","/work"]');
      if (mode === "protected") {
        await expect(panel).toContainText("krs_synthetic");
        await expect(panel).toContainText("Значения Secret не раскрываются");
      }
      await page.screenshot({
        path: testInfo.outputPath(`kubernetes-gate-${mode}.png`),
        fullPage: true,
      });
      if (mode !== "protected") {
        await page.locator('.decision-option input[value="APPROVE"]').check();
        await page
          .locator(".decision-actions")
          .getByRole("button", { name: "Одобрить", exact: true })
          .click();
        await expect.poll(() => writes.length).toBe(1);
        expect(writes[0]).toEqual({ decision: "APPROVE" });
        await expect(page.locator(".decision-detail")).toHaveCount(0);
      }
    }
    expect(failures).toEqual([]);
  });
}
