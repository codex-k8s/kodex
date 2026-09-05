import { expect, test } from "@playwright/test";
import type {
  EmailMailboxActionAvailability,
  EmailMailboxConfigurationView,
  EmailMailboxDraftInput,
} from "../src/shared/api/generated/openapi/types.gen";

for (const width of [390, 2900]) {
  test(`synthetic: почтовый черновик и separate delivery ${String(width)}px`, async ({
    page,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    let current: EmailMailboxConfigurationView | undefined;
    let firstInput: EmailMailboxDraftInput | undefined;
    let firstKey = "";
    let creates = 0;
    let bindCalls = 0;
    let deliveryReads = 0;
    let copies = 0;
    const actions = (
      state: string,
      git = false,
    ): EmailMailboxConfigurationView["nextActions"] => {
      const availability = (
        action: EmailMailboxActionAvailability["action"],
      ): EmailMailboxActionAvailability => {
        const enabled = git
          ? action === "COPY" || action === "DETACH"
          : action === "CREATE_DRAFT" ||
            (action === "SAVE" || action === "VALIDATE" || action === "DISCARD"
              ? ["DRAFT", "VALID"].includes(state)
              : action === "PUBLISH"
                ? state === "VALID"
                : action === "BIND"
                  ? state === "PUBLISHED"
                  : false);
        return { action, enabled, reason: enabled ? "NONE" : "STATE" };
      };
      return [
        availability("CREATE_DRAFT"),
        availability("SAVE"),
        availability("VALIDATE"),
        availability("PUBLISH"),
        availability("DISCARD"),
        availability("BIND"),
        availability("UNBIND"),
        availability("DETACH"),
        availability("COPY"),
      ];
    };
    await context.addCookies([
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
      const root =
        "/api/v1/integration-connections/connection_synthetic/email-mailbox";
      if (url.pathname === root + "/configurations") {
        await route.fulfill({
          json: {
            items: current ? [current] : [],
            total: current ? 1 : 0,
            nextPageToken: "",
            nextActions: [
              { action: "CREATE_DRAFT", enabled: true, reason: "NONE" },
            ],
          },
        });
        return;
      }
      if (url.pathname === root + "/credentials") {
        await route.fulfill({
          json: { items: [], total: 0, nextPageToken: "" },
        });
        return;
      }
      if (url.pathname === root + "/configuration") {
        if (!current) {
          await route.fulfill({
            status: 404,
            json: { status: 404, code: "NOT_FOUND", retryable: false },
          });
          return;
        }
        if (current.publication?.state === "PENDING" && ++deliveryReads >= 2)
          current.publication = {
            ...current.publication,
            state: "READY",
            readyAt: "2026-09-05T00:00:01Z",
          };
        await route.fulfill({ json: current });
        return;
      }
      if (url.pathname === root + "/drafts") {
        const input = request.postDataJSON() as EmailMailboxDraftInput;
        creates++;
        if (!current) {
          firstInput = input;
          firstKey = request.headers()["idempotency-key"] ?? "";
          expect(firstKey).toBeTruthy();
          expect(request.headers()["if-match"]).toBeUndefined();
          current = {
            connectionRef: "connection_synthetic",
            connectionVersion: 3,
            mailboxRef: "mailbox_synthetic",
            configuration: {
              ref: "configuration_synthetic",
              version: 1,
              kind: "EMAIL_MAILBOX",
              name: input.name,
              managedBy: "UI",
              source: "",
              sourceRevision: "",
              updatedAt: "2026-09-05T00:00:00Z",
            },
            revision: {
              ref: "revision_synthetic",
              revision: 1,
              state: "DRAFT",
              contentFormat: "YAML",
              content: "enabled: false\nreceiveProtocol: IMAP\n",
              digest: "a".repeat(64),
              validationDiagnostics: [],
              createdAt: "2026-09-05T00:00:00Z",
            },
            specification: input.content.specification ?? {},
            diagnostics: [],
            boundRevisionRef: "",
            nextActions: actions("DRAFT"),
          };
          await route.abort("failed");
          return;
        }
        expect(input).toEqual(firstInput);
        expect(request.headers()["idempotency-key"]).toBe(firstKey);
        await route.fulfill({
          status: 201,
          headers: { ETag: '"1"' },
          json: current,
        });
        return;
      }
      if (
        url.pathname ===
          "/api/v1/managed-configurations/configuration_synthetic/copies" &&
        current
      ) {
        expect(request.headers()["if-match"]).toBe(
          `"${String(current.configuration.version)}"`,
        );
        expect(request.postDataJSON()).toEqual({ name: "Копия почты" });
        copies++;
        current = {
          ...current,
          mailboxRef: "mailbox_copy",
          boundRevisionRef: "",
          configuration: {
            ...current.configuration,
            ref: "configuration_copy",
            version: 1,
            name: "Копия почты",
            managedBy: "UI",
            source: "",
            sourceRevision: "",
          },
          revision: {
            ...current.revision,
            ref: "revision_copy",
            revision: 1,
            state: "DRAFT",
            parentRevisionRef: "revision_synthetic",
          },
          nextActions: actions("DRAFT"),
        };
        await route.fulfill({
          status: 201,
          headers: { ETag: '"1"' },
          json: {
            configuration: current.configuration,
            revision: current.revision,
          },
        });
        return;
      }
      if (
        url.pathname.startsWith(
          "/api/v1/email-mailbox-configurations/configuration_synthetic/revisions/revision_synthetic/",
        ) &&
        current
      ) {
        expect(request.headers()["if-match"]).toBe(
          `"${String(current.configuration.version)}"`,
        );
        expect(request.headers()["idempotency-key"]).toBeTruthy();
        const action = url.pathname.split("/").at(-1);
        if (action === "validation") current.revision.state = "VALID";
        else if (action === "publication") current.revision.state = "PUBLISHED";
        else if (action === "binding") {
          expect(current.revision.state).toBe("PUBLISHED");
          expect(current.publication).toBeUndefined();
          expect(request.postDataJSON()).toEqual({
            connectionRef: "connection_synthetic",
            expectedConnectionVersion: 3,
          });
          bindCalls++;
          current.connectionVersion = 4;
          current.boundRevisionRef = current.revision.ref;
          current.publication = {
            ref: "publication_synthetic",
            revision: 1,
            digest: "b".repeat(64),
            state: "PENDING",
            configurationRevisionRef: current.revision.ref,
            createdAt: "2026-09-05T00:00:00Z",
            failureCode: "",
          };
        } else {
          failures.push("Unexpected command");
          await route.abort();
          return;
        }
        current.configuration.version++;
        current.nextActions = actions(current.revision.state);
        await route.fulfill({
          headers: { ETag: `"${String(current.configuration.version)}"` },
          json: current,
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      const response = await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      });
      await route.fulfill({ response });
    });
    await page.goto("/e2e/fixtures/mailbox.html");
    const panel = page.getByRole("region", { name: "Почтовая конфигурация" });
    await panel.getByLabel("Название", { exact: true }).fill("Почта проекта");
    await panel.getByLabel("Протокол получения").selectOption("IMAP");
    await panel
      .getByRole("button", { name: "Сохранить новый черновик", exact: true })
      .click();
    await expect(panel.getByText(/Исход команды неизвестен/)).toBeVisible();
    await panel
      .getByRole("button", { name: "Повторить исходную команду" })
      .click();
    await expect(panel.getByText(/Исход команды неизвестен/)).toHaveCount(0);
    expect(creates).toBe(2);
    await panel
      .getByRole("button", { name: "Проверить черновик", exact: true })
      .click();
    await panel
      .getByRole("button", { name: "Опубликовать ревизию", exact: true })
      .click();
    await expect(
      panel.getByRole("button", {
        name: "Применить к подключению",
        exact: true,
      }),
    ).toBeEnabled();
    await panel
      .getByRole("button", { name: "Применить к подключению", exact: true })
      .click();
    await expect.poll(() => current?.publication?.state).toBe("READY");
    expect(bindCalls).toBe(1);
    await expect(
      panel.getByText("publication_synthetic", { exact: true }),
    ).toHaveCount(0);
    await expect(panel.getByLabel("Протокол получения")).toBeDisabled();
    await page.reload();
    await expect(
      panel.getByText("Применённая ревизия:", { exact: false }),
    ).toContainText("revision_synthetic");
    const published = current as EmailMailboxConfigurationView;
    published.configuration.managedBy = "GIT";
    published.configuration.source = "git";
    published.configuration.sourceRevision = "synthetic-commit";
    published.nextActions = actions("PUBLISHED", true);
    await page.reload();
    await panel.getByLabel("Имя копии конфигурации").fill("Копия почты");
    await panel
      .getByRole("button", { name: "Создать копию", exact: true })
      .click();
    await expect(panel.getByLabel("Название", { exact: true })).toHaveValue(
      "Копия почты",
    );
    await expect(
      panel.getByText("Применённая ревизия:", { exact: false }),
    ).toContainText("—");
    await expect(panel.getByLabel("Протокол получения")).toBeEnabled();
    expect(copies).toBe(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`mailbox-${String(width)}.png`),
      fullPage: true,
    });
    expect(failures).toEqual([]);
  });
}
