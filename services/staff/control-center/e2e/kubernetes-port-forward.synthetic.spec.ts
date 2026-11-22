import { createHash } from "node:crypto";
import { expect, test } from "@playwright/test";
import {
  input,
  connection,
  submission,
} from "../src/shared/api/kubernetes-port-forward-fixtures.test-support";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
for (const mode of [
  "connections",
  "lost-open",
  "lost-close",
  "visibility",
] as const) {
  test(`Kubernetes exact PF controls ${mode}`, async ({ page }) => {
    await page.setViewportSize({
      width: mode === "connections" ? 390 : 1440,
      height: 1100,
    });
    await installSyntheticCsrf(page.context(), "p");
    let current = { ...input(), runRef: "run_catalog_1" },
      selected = connection(),
      opened = false,
      closed = false,
      openCount = 0,
      closeCount = 0,
      dataCount = 0,
      openReads = 0,
      denied = false,
      openKey = "";
    let stage = submission();
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (
        request.method() === "GET" &&
        [
          "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-tools",
          "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-captures",
        ].includes(url.pathname)
      ) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
        await route.fulfill({ json: { items: [current], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/input") && request.method() === "GET") {
        await route.fulfill({
          json: {
            view: current,
            ...(url.searchParams.has("submissionIdempotencyKey")
              ? { submission: stage }
              : {}),
          },
        });
        return;
      }
      if (
        url.pathname.endsWith("/port-forward-connections") &&
        request.method() === "GET"
      ) {
        if (denied) {
          await route.fulfill({
            status: 403,
            json: {
              type: "about:blank",
              title: "Forbidden",
              status: 403,
              code: "FORBIDDEN",
            },
          });
          return;
        }
        await route.fulfill({
          json: {
            items: opened ? [selected] : [],
            nextPageToken: "",
            profile: {
              protocol: "PORT_FORWARD_SPDY_V1",
              maximumActiveConnections: 16,
              activeConnections: opened && !closed ? 1 : 0,
              admissionReasonCode: "",
              ports: [
                { port: 18080, targetPort: 8080 },
                { port: 19090, targetPort: 9090 },
              ],
              timeoutSeconds: 120,
            },
          },
        });
        return;
      }
      if (
        url.pathname.endsWith("/port-forward-connections") &&
        request.method() === "POST"
      ) {
        openCount++;
        expect(openCount).toBe(1);
        expect(request.postDataJSON()).toEqual({ slot: 1 });
        expect(request.headers()["if-match"]).toBe('"4"');
        openKey = request.headers()["idempotency-key"] ?? "";
        expect(openKey).not.toBe("");
        current = { ...current, version: 5 };
        selected = { ...selected, slot: 1, state: "OPEN", inputVersion: 5 };
        stage = { ...stage, state: "ACKNOWLEDGED" };
        opened = true;
        if (mode === "lost-open") {
          await route.fulfill({
            status: 503,
            json: {
              type: "about:blank",
              title: "Unavailable",
              status: 503,
              code: "UNAVAILABLE",
            },
          });
          return;
        }
        await route.fulfill({
          status: 201,
          json: { connection: selected, input: current, submission: stage },
        });
        return;
      }
      if (
        url.pathname.endsWith("/port-forward-connection") &&
        request.method() === "GET"
      ) {
        if (url.searchParams.has("openIdempotencyKey")) {
          openReads++;
          expect(url.searchParams.get("openIdempotencyKey")).toBe(openKey);
        } else
          expect(url.searchParams.get("connectionRef")).toBe(
            selected.connectionRef,
          );
        await route.fulfill({
          json: { connection: selected, submission: stage },
        });
        return;
      }
      if (url.pathname.endsWith("/input") && request.method() === "POST") {
        dataCount++;
        const body = request.postDataJSON() as {
          channel: string;
          payloadBase64: string;
          payloadDigest: string;
          endOfInput: boolean;
        };
        const raw = Buffer.from(body.payloadBase64, "base64");
        expect(body.channel).toBe("PORT_FORWARD");
        expect(body.endOfInput).toBe(false);
        expect(raw.subarray(0, 4).toString("hex")).toBe("4b504601");
        expect(raw[5]).toBe(1);
        expect(raw.readBigUInt64BE(6)).toBe(3n);
        expect(raw.subarray(15, 43).toString()).toBe(selected.connectionRef);
        expect(raw.readBigUInt64BE(43)).toBe(0n);
        expect(body.payloadDigest).toBe(
          createHash("sha256").update(raw).digest("hex"),
        );
        if (dataCount === 1) {
          expect(raw[4]).toBe(2);
          expect(raw.subarray(51).toString()).toBe("привет");
        } else {
          expect(dataCount).toBe(2);
          expect(raw[4]).toBe(3);
          expect(raw.length).toBe(51);
          selected = { ...selected, localWriteClosed: true };
        }
        current = {
          ...current,
          version: current.version + 1,
          acknowledgedSequence: dataCount + 1,
        };
        selected = { ...selected, inputVersion: current.version };
        stage = {
          ...stage,
          sequence: dataCount + 1,
          byteCount: raw.length,
          payloadDigest: body.payloadDigest,
          state: "ACKNOWLEDGED",
        };
        await route.fulfill({
          status: 201,
          json: { view: current, submission: stage },
        });
        return;
      }
      if (
        url.pathname.endsWith(
          `/port-forward-connections/${selected.connectionRef}/close`,
        )
      ) {
        closeCount++;
        expect(closeCount).toBe(1);
        expect(request.postDataJSON()).toEqual({});
        current = { ...current, version: current.version + 1 };
        selected = {
          ...selected,
          state: "CLOSED",
          inputVersion: current.version,
          nativeJoined: false,
        };
        closed = true;
        if (mode === "lost-close") {
          await route.fulfill({
            status: 503,
            json: {
              type: "about:blank",
              title: "Unavailable",
              status: 503,
              code: "UNAVAILABLE",
            },
          });
          return;
        }
        await route.fulfill({ json: { connection: selected, input: current } });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`unexpected ${request.method()} API`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/kubernetes-stream-input.html");
    const parent = page.getByTestId("kubernetes-stream-input");
    await parent.getByRole("button", { name: /Выбрать поток:/ }).click();
    const panel = page.getByTestId("kubernetes-port-forward");
    await expect(
      panel.getByRole("heading", {
        name: "TCP-соединения разрешённого туннеля",
      }),
    ).toBeVisible();
    await expect(
      panel.getByText(/Браузер не открывает локальный TCP listener/),
    ).toBeVisible();
    const open = panel.getByRole("button", {
      name: "Открыть соединение",
      exact: true,
    });
    await expect(open).toBeDisabled();
    await panel.getByLabel("Разрешённая пара портов").selectOption("1");
    await expect(open).toBeEnabled();
    await open.click();
    await expect.poll(() => openCount).toBe(1);
    if (mode === "lost-open") await expect.poll(() => openReads).toBe(1);
    await expect(
      panel.getByLabel("Текстовые байты UTF-8 для выбранного соединения"),
    ).toBeEnabled();
    if (mode === "connections") {
      await panel
        .getByLabel("Текстовые байты UTF-8 для выбранного соединения")
        .fill("привет");
      await panel
        .getByRole("button", { name: "Передать байты", exact: true })
        .click();
      await expect.poll(() => dataCount).toBe(1);
      await expect(panel.locator("textarea")).toHaveValue("");
      await panel
        .getByRole("button", {
          name: "Закрыть направление записи",
          exact: true,
        })
        .click();
      await expect.poll(() => dataCount).toBe(2);
    }
    if (mode === "visibility") {
      denied = true;
      await panel
        .getByRole("button", { name: "Обновить сведения", exact: true })
        .click();
      await expect(panel.locator("select")).toHaveCount(0);
      await expect(panel.locator("textarea")).toHaveCount(0);
      expect(closeCount).toBe(0);
    } else {
      await panel
        .getByRole("button", {
          name: "Закрыть выбранное соединение",
          exact: true,
        })
        .click();
      await expect.poll(() => closeCount).toBe(1);
      await expect(
        panel.getByText(/Завершение native pair не подтверждено/),
      ).toBeVisible();
      await expect(
        panel.getByRole("button", { name: "Передать байты", exact: true }),
      ).toBeDisabled();
    }
    expect(failures).toEqual([]);
    expect(
      await panel.evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
    ).toBe(true);
  });
}
