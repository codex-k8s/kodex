import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import type {
  PersonalIntegrationConnection,
  SshIdentitySetup,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

const material = "synthetic-first-key-line\nsynthetic-second-key-line\n";
const connection: PersonalIntegrationConnection = {
  connectionRef: "int_ssh_fixture01",
  connectionVersion: 7,
  definitionKey: "ssh-managed",
  name: "Личный SSH",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
  credentialDelivery: { kind: "BROKER_IDENTITY", brokerPurpose: "SSH_TARGET" },
};
const view: SshIdentitySetup = {
  ref: "ssu_12345678901234567890123456789012",
  connectionRef: connection.connectionRef,
  connectionVersion: 7,
  identityRef: "bki_fixture_123",
  targetRef: "target_fixture_123",
  identityRevision: 1,
  bindingVersion: 1,
  bindingGeneration: 1,
  policy: "INITIATOR_REQUIRED",
  state: "PREPARED",
  reasonCode: "PREPARED",
  version: 1,
  expiresAt: "2099-11-16T12:00:00Z",
};
const origin = "https://ssh-upload.example.invalid";
const permit = Buffer.from("p".repeat(32)).toString("base64url");
const cspTemplate = readFileSync(
  new URL(
    "../../../../deploy/k8s/base/staff-control-center/security-headers.conf",
    import.meta.url,
  ),
  "utf8",
);
const contentSecurityPolicy = cspTemplate
  .match(/^add_header Content-Security-Policy "(.*)" always;$/m)?.[1]
  ?.replace(
    "__KODEX_OIDC_ORIGIN__",
    `https://identity.example.invalid ${origin}`,
  );
if (!contentSecurityPolicy)
  throw new Error("Synthetic canonical SSH upload CSP is absent");

async function fixture(
  page: Page,
  mode: "success" | "lost-upload" | "lost-begin" | "foreign-origin",
) {
  const calls: string[] = [],
    failures: string[] = [];
  let current = { ...view },
    beginKey = "";
  await installSyntheticCsrf(page.context(), "s");
  page.on("pageerror", () => failures.push("pageerror"));
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (
      url.origin === "https://kodex.test" &&
      (await handleSyntheticRuntimeRequest(route, url, failures))
    )
      return;
    if (url.pathname === "/api/v1/me/integration-connections") {
      await route.fulfill({ json: { items: [connection] } });
      return;
    }
    const path = `/api/v1/integration-connections/${connection.connectionRef}/ssh-identity-setups`;
    if (url.pathname === path && request.method() === "POST") {
      calls.push("BEGIN");
      expect(request.headers()["if-match"]).toBe('"0"');
      expect(request.headers()["x-csrf-token"]).toBe("s".repeat(43));
      beginKey = request.headers()["idempotency-key"] ?? "";
      expect(beginKey).toBeTruthy();
      expect(request.postDataJSON()).toEqual({
        expectedConnectionVersion: 7,
        contentSHA256: createHash("sha256").update(material).digest("hex"),
        contentBytes: Buffer.byteLength(material),
      });
      if (mode === "lost-begin") await route.abort("failed");
      else
        await route.fulfill({
          status: 201,
          json: {
            setup: current,
            reconcileOnly: false,
            upload: {
              origin:
                mode === "foreign-origin"
                  ? "https://foreign.example.invalid"
                  : origin,
              tlsServerName:
                mode === "foreign-origin"
                  ? "foreign.example.invalid"
                  : "ssh-upload.example.invalid",
              permit,
            },
          },
        });
      return;
    }
    if (
      request.method() === "GET" &&
      (url.pathname === path || url.pathname === `${path}/${view.ref}`)
    ) {
      calls.push("READ");
      if (mode === "lost-begin") {
        expect(url.searchParams.get("beginIdempotencyKey")).toBe(beginKey);
        current = {
          ...current,
          state: "UNREPORTED",
          reasonCode: "MISSING_COMPLETION",
          version: 2,
        };
      }
      await route.fulfill({ json: current });
      return;
    }
    if (url.origin === origin) {
      const cors = {
        "access-control-allow-origin": "https://kodex.test",
        "access-control-allow-methods": "POST",
        "access-control-allow-headers": "authorization,content-type",
      };
      if (request.method() === "OPTIONS") {
        await route.fulfill({ status: 204, headers: cors });
        return;
      }
      expect(request.method()).toBe("POST");
      expect(request.headers()["authorization"]).toBe(`Bearer ${permit}`);
      expect(request.headers()["cookie"]).toBeUndefined();
      expect(request.headers()["referer"]).toBeUndefined();
      if (url.pathname === `/v1/ssh-identities/${view.ref}/material`) {
        calls.push("UPLOAD");
        expect(request.postData()).toBe(material);
        current =
          mode === "lost-upload"
            ? {
                ...current,
                state: "UNKNOWN",
                reasonCode: "MATERIAL_UNKNOWN",
                version: 2,
              }
            : {
                ...current,
                state: "COMPLETED",
                reasonCode: "COMPLETED",
                version: 2,
              };
        if (mode === "lost-upload") await route.abort("failed");
        else await route.fulfill({ status: 200, headers: cors, body: "{}" });
        return;
      }
      if (url.pathname === `/v1/ssh-identities/${view.ref}/observe`) {
        calls.push("OBSERVE");
        expect(request.postData()).toBeNull();
        current = {
          ...current,
          state: "COMPLETED",
          reasonCode: "COMPLETED",
          version: 3,
        };
        await route.fulfill({ status: 200, headers: cors, body: "{}" });
        return;
      }
    }
    if (
      url.pathname.startsWith("/api/") ||
      url.origin === origin ||
      url.origin === "https://foreign.example.invalid"
    ) {
      failures.push("unexpected protected route");
      await route.abort();
      return;
    }
    const response = await route.fetch({
      url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
    });
    await route.fulfill({
      response,
      headers: {
        ...response.headers(),
        ...(request.resourceType() === "document"
          ? { "content-security-policy": contentSecurityPolicy }
          : {}),
      },
    });
  });
  await page.goto("/e2e/fixtures/personal-connections.html");
  await expect(
    page.getByRole("heading", { name: "SSH-ключ", exact: true }),
  ).toBeVisible();
  return { calls, failures };
}

for (const width of [1440, 390]) {
  test(`SSH identity: прямой TLS upload и owner readback без generic TOKEN ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 950 });
    const f = await fixture(page, "success");
    const input = page.getByRole("textbox", { name: /Закрытый SSH-ключ/ });
    await expect(input).toHaveJSProperty("tagName", "TEXTAREA");
    await input.fill(material);
    await page
      .getByRole("button", { name: "Передать ключ", exact: true })
      .click();
    await expect(
      page
        .getByRole("region", { name: "SSH-ключ", exact: true })
        .getByRole("status"),
    ).toContainText("Ключ привязан.");
    await expect(input).toHaveValue("");
    expect(f.calls).toEqual(["BEGIN", "UPLOAD", "READ"]);
    expect(f.failures).toEqual([]);
    expect(await page.locator("body").innerText()).not.toContain(material);
    expect(
      await page.evaluate(() =>
        JSON.stringify([
          Object.entries(localStorage),
          Object.entries(sessionStorage),
        ]),
      ),
    ).not.toContain("synthetic-first-key-line");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}

test("SSH identity: потеря upload ACK разрешает только явный Observe, без второй записи", async ({
  page,
}) => {
  const f = await fixture(page, "lost-upload");
  await page.getByRole("textbox", { name: /Закрытый SSH-ключ/ }).fill(material);
  await page
    .getByRole("button", { name: "Передать ключ", exact: true })
    .click();
  await expect(
    page
      .getByRole("region", { name: "SSH-ключ", exact: true })
      .getByRole("status"),
  ).toContainText("Исход записи неизвестен.");
  await page
    .getByRole("button", { name: "Уточнить исход записи", exact: true })
    .click();
  await expect(
    page
      .getByRole("region", { name: "SSH-ключ", exact: true })
      .getByRole("status"),
  ).toContainText("Ключ привязан.");
  expect(f.calls).toEqual([
    "BEGIN",
    "UPLOAD",
    "READ",
    "READ",
    "OBSERVE",
    "READ",
  ]);
  expect(f.failures).toEqual([]);
});

test("SSH identity: потеря Begin ACK очищает ключ и восстанавливает metadata без upload", async ({
  page,
}) => {
  const f = await fixture(page, "lost-begin");
  const input = page.getByRole("textbox", { name: /Закрытый SSH-ключ/ });
  await input.fill(material);
  await page
    .getByRole("button", { name: "Передать ключ", exact: true })
    .click();
  await expect(input).toHaveValue("");
  await page
    .getByRole("button", { name: "Проверить состояние", exact: true })
    .click();
  await expect(
    page
      .getByRole("region", { name: "SSH-ключ", exact: true })
      .getByRole("status"),
  ).toContainText("Подтверждение владельца не получено.");
  expect(f.calls).toEqual(["BEGIN", "READ"]);
  expect(f.failures).toEqual([]);
});

test("SSH identity: canonical CSP запрещает передачу ключа чужому origin", async ({
  page,
}) => {
  const f = await fixture(page, "foreign-origin");
  const input = page.getByRole("textbox", { name: /Закрытый SSH-ключ/ });
  await input.fill(material);
  await page
    .getByRole("button", { name: "Передать ключ", exact: true })
    .click();
  await expect(
    page
      .getByRole("region", { name: "SSH-ключ", exact: true })
      .getByRole("status"),
  ).toContainText("Настройка создана;");
  await expect(input).toHaveValue("");
  expect(f.calls).toEqual(["BEGIN", "READ"]);
  expect(f.failures).toEqual([]);
});
