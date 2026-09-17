import { expect, test } from "@playwright/test";

import type {
  BootstrapState,
  SttModelCatalog,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";

const environment = loadE2EAuthEnvironment();

test("локальный STT публикует каталог без credential и закрыто отклоняет audio до настройки", async ({
  page,
}) => {
  const serverFailures: string[] = [];
  page.on("response", (response) => {
    if (response.status() >= 500) {
      serverFailures.push(
        `${String(response.status())} ${new URL(response.url()).pathname}`,
      );
    }
  });

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

  const readback = await page.evaluate(async () => {
    const bootstrapResponse = await fetch("/api/v1/bootstrap", {
      cache: "no-store",
    });
    const catalogResponse = await fetch("/api/v1/system-stt/model-catalog", {
      cache: "no-store",
    });
    const csrfPrefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
    const csrf = document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(csrfPrefix))
      ?.slice(csrfPrefix.length);
    if (!csrf) throw new Error("CSRF token is unavailable");
    const invalidAudioResponse = await fetch("/api/v1/speech/transcriptions", {
      method: "POST",
      headers: {
        "Content-Type": "application/octet-stream",
        "X-Audio-Size": "1",
        "X-CSRF-Token": decodeURIComponent(csrf),
      },
      body: "x",
    });
    return {
      bootstrapStatus: bootstrapResponse.status,
      bootstrap: (await bootstrapResponse.json()) as BootstrapState,
      catalogStatus: catalogResponse.status,
      catalog: (await catalogResponse.json()) as SttModelCatalog,
      invalidAudioStatus: invalidAudioResponse.status,
      invalidAudio: (await invalidAudioResponse.json()) as { code?: string },
    };
  });

  expect(readback.bootstrapStatus).toBe(200);
  expect(readback.bootstrap).toMatchObject({
    speechTranscription: {
      available: false,
      reason: "STT_NOT_CONFIGURED",
    },
  });
  expect(readback.catalogStatus, JSON.stringify(readback.catalog)).toBe(200);
  expect(readback.catalog).toMatchObject({
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
  expect(readback.catalog.models).toHaveLength(
    new Set(readback.catalog.models.map((model) => model.model)).size,
  );
  expect(readback.invalidAudioStatus).toBe(415);
  expect(readback.invalidAudio).toMatchObject({
    code: "UNSUPPORTED_MEDIA_TYPE",
  });
  expect(serverFailures).toEqual([]);
});
