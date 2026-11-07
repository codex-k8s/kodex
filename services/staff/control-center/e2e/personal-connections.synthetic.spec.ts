import { isRecord } from "../src/shared/lib/exact-object";
import { expect, test, type Page } from "@playwright/test";
import type {
  PersonalIntegrationConnection,
  PersonalIntegrationCredential,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

const connection: PersonalIntegrationConnection = {
  connectionRef: "int_fixture01",
  connectionVersion: 7,
  name: "GitLab для проверки",
  definitionKey: "gitlab",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
};
const credential: PersonalIntegrationCredential = {
  connectionRef: connection.connectionRef,
  connectionVersion: 7,
  bindingRef: "ipc_fixture01",
  version: 1,
  policy: "INITIATOR_REQUIRED",
  state: "NOT_CONFIGURED",
  credentialConfigured: false,
  verifiedCapabilityKeys: [],
  deniedCapabilityKeys: [],
};

async function setup(
  page: Page,
  initial: PersonalIntegrationConnection = connection,
  failSave = false,
  empty = false,
) {
  const failures: string[] = [];
  let current = structuredClone(initial);
  function readCredential(): PersonalIntegrationCredential {
    const value = current.credential;
    if (!value) throw new Error("Synthetic credential is absent");
    return value;
  }
  let writes = 0;
  const saved: string[] = [];
  page.on("pageerror", () => failures.push("pageerror"));
  await installSyntheticCsrf(page.context(), "p");
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname.startsWith("/api/")) {
      if (
        url.pathname === "/api/v1/me/integration-connections" &&
        request.method() === "GET"
      ) {
        // Последняя страница намеренно использует wire-форму без optional курсора.
        await route.fulfill({ json: { items: empty ? [] : [current] } });
        return;
      }
      if (
        !url.pathname.startsWith(
          `/api/v1/me/integration-connections/${connection.connectionRef}/credential`,
        )
      ) {
        failures.push("unexpected API");
        await route.abort();
        return;
      }
      const body: unknown = request.postDataJSON();
      if (!isRecord(body)) throw new Error("Synthetic request body is invalid");
      writes++;
      expect(request.headers()["x-csrf-token"]).toBe("p".repeat(43));
      expect(request.headers()["idempotency-key"]).toBeTruthy();
      expect(body).not.toHaveProperty("subjectId");
      expect(body).not.toHaveProperty("secretRef");
      if (request.method() === "POST" && url.pathname.endsWith("/credential")) {
        expect(body).toEqual({ expectedConnectionVersion: 7 });
        current = { ...current, credential: structuredClone(credential) };
        await route.fulfill({ status: 201, json: current.credential });
        return;
      }
      expect(request.headers()["if-match"]).toBe(
        `"${String(readCredential().version)}"`,
      );
      if (request.method() === "PUT") {
        if (typeof body.value !== "string")
          throw new Error("Synthetic key is absent");
        saved.push(body.value);
        expect(body.expectedConnectionVersion).toBe(7);
        expect(Object.keys(body).sort()).toEqual([
          "expectedConnectionVersion",
          "value",
        ]);
        if (failSave) {
          await route.abort("failed");
          return;
        }
        current = {
          ...current,
          credential: {
            ...readCredential(),
            version: readCredential().version + 1,
            state: "CHECK_REQUIRED",
            credentialConfigured: true,
            verifiedCapabilityKeys: [],
            deniedCapabilityKeys: [],
          },
        };
      } else if (url.pathname.endsWith("/test")) {
        expect(body).toEqual({ expectedConnectionVersion: 7 });
        current = {
          ...current,
          credential: {
            ...readCredential(),
            version: readCredential().version + 1,
            state: "CHECKING",
          },
        };
      } else if (url.pathname.endsWith("/revoke")) {
        expect(body).toEqual({});
        current = {
          ...current,
          credential: {
            ...readCredential(),
            version: readCredential().version + 1,
            state: "REVOKED",
            credentialConfigured: false,
            verifiedCapabilityKeys: [],
            deniedCapabilityKeys: [],
          },
        };
      } else {
        failures.push("unexpected operation");
        await route.abort();
        return;
      }
      await route.fulfill({ json: current.credential });
      return;
    }
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto("/e2e/fixtures/personal-connections.html");
  if (!empty) {
    await expect(
      page.getByRole("heading", { name: connection.name }),
    ).toBeVisible();
  }
  return {
    failures,
    get writes() {
      return writes;
    },
    get saved() {
      return saved;
    },
    ready() {
      current = {
        ...current,
        credential: {
          ...readCredential(),
          version: readCredential().version + 1,
          state: "READY",
          verifiedCapabilityKeys: ["gitlab.identity.read"],
        },
      };
    },
  };
}

for (const width of [1440, 390]) {
  test(`пустой каталог без курсора показывает empty state, ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await setup(page, connection, false, true);
    await expect(
      page.getByText("Доступных подключений пока нет.", { exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("alert")).toHaveCount(0);
    expect(fixture.writes).toBe(0);
    expect(fixture.failures).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}

test("права Jira проверяются в контексте выбранной записи", async ({
  page,
}) => {
  const fixture = await setup(page, {
    ...connection,
    definitionKey: "jira-server-dc",
    credential: {
      ...credential,
      credentialConfigured: true,
      state: "READY",
      verifiedCapabilityKeys: ["jira.project.read", "jira.identity.read"],
    },
  });
  await expect(
    page.getByText("Чтение выбранного файла, задачи или страницы", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Перед созданием задачи проверяются права", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("jira.project.read", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("jira.issue.create", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("Обновление страницы проверяет личные права", {
      exact: false,
    }),
  ).toHaveCount(0);
  expect(fixture.writes).toBe(0);
  expect(fixture.failures).toEqual([]);
});

test("права на страницу проверяются отдельно от подтверждённого чтения", async ({
  page,
}) => {
  const fixture = await setup(page, {
    ...connection,
    definitionKey: "confluence-server-dc",
    credential: {
      ...credential,
      credentialConfigured: true,
      state: "READY",
      verifiedCapabilityKeys: [
        "confluence.space.read",
        "confluence.identity.read",
      ],
    },
  });
  await expect(
    page.getByText("Чтение выбранного файла, задачи или страницы", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Обновление страницы проверяет личные права", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("confluence.space.read", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("confluence.page.update", { exact: true }),
  ).toHaveCount(0);
  expect(fixture.writes).toBe(0);
  expect(fixture.failures).toEqual([]);
});

test("личный ключ: добавить, проверить, readback, заменить и отозвать", async ({
  page,
}, testInfo) => {
  const fixture = await setup(page);
  await expect(
    page.getByText("Создание Issue, ветки или MR требует согласования", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Чтение выбранного файла, задачи или страницы", {
      exact: false,
    }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Добавить ключ", exact: true })
    .click();
  await page
    .getByLabel("Личный ключ API", { exact: true })
    .fill("synthetic-private-key-one");
  await page
    .getByRole("button", { name: "Сохранить ключ", exact: true })
    .click();
  await expect(page.getByRole("status")).toHaveText(
    "Ключ сохранён. Выполните проверку доступа.",
  );
  await expect(page.getByLabel("Личный ключ API", { exact: true })).toHaveCount(
    0,
  );
  expect(await page.locator("body").innerText()).not.toContain(
    "synthetic-private-key-one",
  );
  await page
    .getByRole("button", { name: "Проверить доступ", exact: true })
    .click();
  await expect(
    page.getByText("Проверка выполняется; обновите сведения для результата", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Проверить доступ", exact: true }),
  ).toHaveCount(0);
  fixture.ready();
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByText("gitlab.identity.read", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/Остальные права остаются непроверенными/),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("personal-connections-ready.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Заменить ключ", exact: true })
    .click();
  await expect(page.getByLabel("Личный ключ API", { exact: true })).toHaveValue(
    "",
  );
  await page
    .getByLabel("Личный ключ API", { exact: true })
    .fill("synthetic-private-key-two");
  await page
    .getByRole("button", { name: "Сохранить ключ", exact: true })
    .click();
  await expect(page.getByRole("status")).toHaveText(
    "Ключ сохранён. Выполните проверку доступа.",
  );
  await page
    .getByRole("button", { name: "Отозвать мой ключ", exact: true })
    .click();
  await expect(
    page.getByText("Ключ отозван для новых вызовов", { exact: true }),
  ).toBeVisible();
  expect(fixture.writes).toBe(5);
  expect(fixture.saved).toEqual([
    "synthetic-private-key-one",
    "synthetic-private-key-two",
  ]);
  expect(fixture.failures).toEqual([]);
});

test("ошибка сохранения очищает поле и блокирует новый write до readback", async ({
  page,
}) => {
  const fixture = await setup(
    page,
    {
      ...connection,
      credential: {
        ...credential,
        credentialConfigured: true,
        state: "CHECK_REQUIRED",
      },
    },
    true,
  );
  await page
    .getByRole("button", { name: "Заменить ключ", exact: true })
    .click();
  await page
    .getByLabel("Личный ключ API", { exact: true })
    .fill("synthetic-lost-key");
  await page
    .getByRole("button", { name: "Сохранить ключ", exact: true })
    .click();
  await expect(
    page.getByText(/Результат операции не подтверждён/),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Проверить доступ", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Заменить ключ", exact: true }),
  ).toBeDisabled();
  await expect(page.getByLabel("Личный ключ API", { exact: true })).toHaveCount(
    0,
  );
  expect(fixture.writes).toBe(1);
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Заменить ключ", exact: true }),
  ).toBeEnabled();
  expect(fixture.writes).toBe(1);
  expect(fixture.failures).toEqual([]);
});

test("сохранённый личный ключ можно отозвать после отключения adapter и смены policy", async ({
  page,
}) => {
  const fixture = await setup(page, {
    ...connection,
    available: false,
    policy: "PROJECT_SHARED",
    credential: {
      ...credential,
      policy: "PROJECT_SHARED",
      credentialConfigured: true,
      state: "READY",
      verifiedCapabilityKeys: ["gitlab.identity.read"],
    },
  });
  await expect(
    page.getByRole("button", { name: "Заменить ключ", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Проверить доступ", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Отозвать мой ключ", exact: true })
    .click();
  await expect(
    page.getByText("Ключ отозван для новых вызовов", { exact: true }),
  ).toBeVisible();
  expect(fixture.writes).toBe(1);
  expect(fixture.failures).toEqual([]);
});
