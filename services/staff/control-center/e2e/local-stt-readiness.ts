import { expect, test, type Page } from "@playwright/test";

import type {
  BootstrapState,
  SttModelCatalog,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";

const environment = loadE2EAuthEnvironment();

async function authenticateLocalOwner(page: Page): Promise<void> {
  const session = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/session/callback",
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  expect((await session).status()).toBe(200);
}

function collectServerFailures(page: Page): string[] {
  const failures: string[] = [];
  page.on("response", (response) => {
    if (response.status() >= 500) {
      failures.push(
        `${String(response.status())} ${new URL(response.url()).pathname}`,
      );
    }
  });
  return failures;
}

test("MVP-UI-55: STT unit публикует локальный каталог без provider credential", async ({
  page,
}) => {
  const serverFailures = collectServerFailures(page);
  await authenticateLocalOwner(page);
  const readback = await page.evaluate(async () => {
    const response = await fetch("/api/v1/system-stt/model-catalog", {
      cache: "no-store",
    });
    return {
      status: response.status,
      body: (await response.json()) as SttModelCatalog,
    };
  });

  expect(readback.status, JSON.stringify(readback.body)).toBe(200);
  expect(readback.body).toMatchObject({
    version: expect.any(String),
    observedAt: expect.any(String),
    recommendedModel: expect.any(String),
    responseFormat: expect.any(String),
    recommendedMaximumAudioBytes: expect.any(Number),
    recommendedMaximumAudioDurationMilliseconds: expect.any(Number),
    models: expect.arrayContaining([
      expect.objectContaining({
        model: expect.any(String),
        parameterNames: expect.any(Array),
        chunkingStrategies: expect.any(Array),
      }),
    ]),
  });
  expect(readback.body.models).toHaveLength(
    new Set(readback.body.models.map((model) => model.model)).size,
  );
  expect(serverFailures).toEqual([]);
});

test("MVP-UI-57: gateway отклоняет неверный audio envelope до STT RPC", async ({
  page,
}) => {
  const serverFailures = collectServerFailures(page);
  await authenticateLocalOwner(page);
  const readback = await page.evaluate(async () => {
    const csrfPrefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
    const csrf = document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(csrfPrefix))
      ?.slice(csrfPrefix.length);
    if (!csrf) throw new Error("CSRF token is unavailable");
    const response = await fetch("/api/v1/speech/transcriptions", {
      method: "POST",
      headers: {
        "Content-Type": "application/octet-stream",
        "X-Audio-Size": "1",
        "X-CSRF-Token": decodeURIComponent(csrf),
      },
      body: "x",
    });
    return {
      status: response.status,
      body: (await response.json()) as { code?: string },
    };
  });

  expect(readback.status).toBe(415);
  expect(readback.body).toMatchObject({ code: "UNSUPPORTED_MEDIA_TYPE" });
  expect(serverFailures).toEqual([]);
});

test("MVP-UI-59: readiness закрыто сообщает отсутствие STT-конфигурации", async ({
  page,
}) => {
  const serverFailures = collectServerFailures(page);
  await authenticateLocalOwner(page);
  const readback = await page.evaluate(async () => {
    const response = await fetch("/api/v1/bootstrap", { cache: "no-store" });
    return {
      status: response.status,
      body: (await response.json()) as BootstrapState,
    };
  });

  expect(readback.status).toBe(200);
  expect(readback.body).toMatchObject({
    speechTranscription: {
      available: false,
      reason: "STT_NOT_CONFIGURED",
    },
  });
  expect(serverFailures).toEqual([]);
});
