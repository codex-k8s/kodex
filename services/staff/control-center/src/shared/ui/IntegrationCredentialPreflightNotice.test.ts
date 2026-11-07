import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createRouter, createMemoryHistory } from "vue-router";
import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import IntegrationCredentialPreflightNotice from "./IntegrationCredentialPreflightNotice.vue";
import ProblemNotice from "./ProblemNotice.vue";
import { AppProblem } from "@/shared/api/problem";

describe("preflight launch→настройки", () => {
  it("общая ошибка помощника показывает полный preflight и настройки без retry", async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/", component: { render: () => null } },
        {
          path: "/integrations",
          name: "integrations",
          component: { render: () => null },
        },
      ],
    });
    await router.push("/");
    await router.isReady();
    const problem = new AppProblem({
      status: 409,
      code: "INTEGRATION_CREDENTIALS_REQUIRED",
      kind: "conflict",
      retryable: false,
      integrationCredentialIssues: [
        {
          connectionRef: "int_fixture01",
          connectionName: "GitLab для проверки",
          capabilityKey: "gitlab.project.metadata.read",
          problem: "CREDENTIAL_REVOKED",
        },
        {
          connectionRef: "int_fixture02",
          connectionName: "Jira для проверки",
          capabilityKey: "jira.issue.read",
          problem: "CREDENTIAL_MISSING",
        },
      ],
    });
    const app = createSSRApp({
      render: () => h(ProblemNotice, { problem, compact: true }),
    });
    app.use(router);
    app.use(i18n);
    const html = await renderToString(app);
    expect(html).toContain("GitLab для проверки");
    expect(html).toContain("Jira для проверки");
    expect(html).toContain("Ключ отозван; добавьте новый");
    expect(html).toContain('href="/integrations?section=personal"');
    expect(html).toContain('target="_blank"');
    expect(html).not.toContain("button--secondary");
  });
  it("показывает все подключения и открывает настройки отдельно, сохраняя форму запуска", async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/", component: { render: () => null } },
        {
          path: "/integrations",
          name: "integrations",
          component: { render: () => null },
        },
      ],
    });
    await router.push("/");
    await router.isReady();
    const app = createSSRApp({
      render: () =>
        h(IntegrationCredentialPreflightNotice, {
          issues: [
            {
              connectionRef: "int_fixture01",
              connectionName: "GitLab для проверки",
              capabilityKey: "gitlab.project.metadata.read",
              problem: "CREDENTIAL_REVOKED",
            },
            {
              connectionRef: "int_fixture02",
              connectionName: "Jira для проверки",
              capabilityKey: "jira.issue.read",
              problem: "CREDENTIAL_MISSING",
            },
          ],
        }),
    });
    app.use(router);
    app.use(i18n);
    const html = await renderToString(app);
    expect(html).toContain("Процесс не создан");
    expect(html).toContain("GitLab для проверки");
    expect(html).toContain("Jira для проверки");
    expect(html).toContain("Ключ отозван; добавьте новый");
    expect(html).toContain("Добавьте личный ключ");
    expect(html).toContain('href="/integrations?section=personal"');
    expect(html).toContain('target="_blank"');
    expect(html).toContain('rel="noopener"');
    expect(html).not.toContain("i18n:");
  });
});
