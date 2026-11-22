import { createHash } from "node:crypto";
import { expect, test } from "@playwright/test";
import type { KubernetesStreamInputView } from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
function view(): KubernetesStreamInputView {
  return {
    invocationRef: "inv_synthetic",
    streamRef: "kst_synthetic",
    runRef: "run_catalog_1",
    nodeRef: "nod_synthetic",
    generation: 7,
    operation: "kubernetes.exec",
    namespace: "example",
    podName: "web",
    container: "app",
    state: "ACTIVE",
    version: 4,
    inputChannels: ["STDIN", "TERMINAL_RESIZE"],
    closedInputChannels: [],
    maximumFrameBytes: 32768,
    maximumTotalBytes: 65536,
    maximumStagedBytes: 1073741824,
    maximumStagedObjects: 32768,
    acknowledgedSequence: 0,
    hardDeadline: "2099-11-15T12:00:00Z",
    reasonCode: "",
    targetRef: "target_review",
    targetRevision: 2,
    connectionRef: "int_synthetic",
    resource: {
      group: "",
      version: "v1",
      resource: "pods",
      namespace: "example",
      name: "web",
      uid: "30000000-0000-4000-8000-000000000001",
      resourceVersion: "18",
      subresource: "",
      namespaced: true,
      allNamespaces: false,
    },
  };
}
for (const mode of ["input", "lost-input", "watch", "lost-stop"] as const) {
  test(`Kubernetes active session protected INPUT ${mode}`, async ({
    page,
  }) => {
    await page.setViewportSize({
      width: mode === "input" ? 390 : 1440,
      height: 1000,
    });
    await installSyntheticCsrf(page.context(), "k");
    let current = view(),
      submits = 0,
      stops = 0,
      keyReads = 0;
    let submission: unknown;
    let submissionKey = "";
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    if (mode === "watch") {
      current = {
        ...current,
        operation: "kubernetes.watch.all-namespaces",
        namespace: "@all-namespaces",
        podName: "",
        container: "",
        inputChannels: [],
        resource: {
          ...current.resource,
          namespace: "",
          name: "",
          uid: "",
          resourceVersion: "",
          allNamespaces: true,
        },
      };
    }
    await page.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (url.pathname.endsWith("/kubernetes-captures")) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-tools")) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
        expect(url.pathname).toBe(
          "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-stream-inputs",
        );
        await route.fulfill({ json: { items: [current], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/input") && request.method() === "GET") {
        if (url.searchParams.has("submissionIdempotencyKey")) {
          keyReads++;
          expect(url.searchParams.get("submissionIdempotencyKey")).toBe(
            submissionKey,
          );
        }
        await route.fulfill({
          json: {
            view: current,
            ...(url.searchParams.has("submissionIdempotencyKey")
              ? { submission }
              : {}),
          },
        });
        return;
      }
      if (url.pathname.endsWith("/input") && request.method() === "POST") {
        submits++;
        expect(submits).toBe(1);
        const body = request.postDataJSON() as {
          channel: string;
          payloadBase64: string;
          payloadDigest: string;
          endOfInput: boolean;
        };
        expect(Object.keys(body).sort()).toEqual([
          "channel",
          "endOfInput",
          "payloadBase64",
          "payloadDigest",
        ]);
        expect(request.headers()["if-match"]).toBe('"4"');
        submissionKey = request.headers()["idempotency-key"] ?? "";
        const bytes = Buffer.from(body.payloadBase64, "base64");
        expect(bytes.toString()).toBe("привет\n");
        expect(body.payloadDigest).toBe(
          createHash("sha256").update(bytes).digest("hex"),
        );
        expect(body.channel).toBe("STDIN");
        expect(body.endOfInput).toBe(false);
        submission = {
          ref: "kif_synthetic",
          invocationRef: current.invocationRef,
          streamRef: current.streamRef,
          generation: 7,
          sequence: 1,
          channel: body.channel,
          payloadDigest: body.payloadDigest,
          byteCount: bytes.length,
          endOfInput: false,
          state: "ACKNOWLEDGED",
        };
        bytes.fill(0);
        current = { ...current, version: 5, acknowledgedSequence: 1 };
        if (mode === "lost-input")
          await route.fulfill({
            status: 503,
            json: {
              type: "about:blank",
              title: "Unavailable",
              status: 503,
              code: "UNAVAILABLE",
            },
          });
        else
          await route.fulfill({
            status: 201,
            json: { view: current, submission },
          });
        return;
      }
      if (url.pathname.endsWith("/stop") && request.method() === "POST") {
        stops++;
        expect(request.postDataJSON()).toEqual({});
        current = {
          ...current,
          state: "CANCELED",
          version: current.version + 1,
        };
        if (mode === "lost-stop")
          await route.fulfill({
            status: 503,
            json: {
              type: "about:blank",
              title: "Unavailable",
              status: 503,
              code: "UNAVAILABLE",
            },
          });
        else await route.fulfill({ json: current });
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
    const panel = page.getByTestId("kubernetes-stream-input");
    await expect(
      panel.getByRole("heading", {
        name: "Ввод и остановка Kubernetes-инструмента",
      }),
    ).toBeVisible();
    await expect(
      panel.getByRole("button", { name: "Отправить", exact: true }),
    ).toHaveCount(0);
    await panel.getByRole("button", { name: /Выбрать поток:/ }).click();
    await expect(
      panel.getByText("target_review · 2 · int_synthetic", { exact: true }),
    ).toBeVisible();
    if (mode === "input" || mode === "lost-input") {
      await panel
        .getByLabel("Стандартный ввод", { exact: true })
        .fill("привет\n");
      await panel
        .getByRole("button", { name: "Отправить", exact: true })
        .click();
      await expect(
        panel.getByLabel("Стандартный ввод", { exact: true }),
      ).toHaveValue("");
      await expect.poll(() => submits).toBe(1);
      if (mode === "lost-input") await expect.poll(() => keyReads).toBe(1);
      else expect(keyReads).toBe(0);
      await expect(
        panel.getByText(/Подтверждение ввода означает локальную передачу/),
      ).toBeVisible();
    }
    if (mode === "watch") {
      await expect(panel.locator("textarea")).toHaveCount(0);
      await expect(
        panel.getByText("Этот поток не принимает пользовательский ввод.", {
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        panel.getByText("@all-namespaces", { exact: true }),
      ).toBeVisible();
    }
    await panel
      .getByRole("button", { name: "Остановить этот инструмент", exact: true })
      .click();
    await expect(
      panel.getByText("Закрыт владельцем", { exact: true }),
    ).toBeVisible();
    expect(stops).toBe(1);
    await expect(
      panel.getByText(/Завершение удалённого процесса подтверждается отдельно/),
    ).toBeVisible();
    expect(failures).toEqual([]);
  });
}
