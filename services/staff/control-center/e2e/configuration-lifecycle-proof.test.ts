import { createHash } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, expect, test } from "vitest";

import {
  loadLifecycleConfiguration,
  openLifecycleJournal,
  operationOutcome,
  permittedLifecycleRequest,
} from "./configuration-lifecycle-proof";

const directories: string[] = [];
const versions = {
  harness: "1".repeat(40),
  api: "2".repeat(40),
  pwa: "3".repeat(40),
  servingManifestSHA256: "4".repeat(64),
};

afterEach(async () => {
  await Promise.all(
    directories.splice(0).map((path) => rm(path, { recursive: true })),
  );
});

async function fixture() {
  const directory = await mkdtemp(
    join(tmpdir(), "kodex-configuration-lifecycle-"),
  );
  directories.push(directory);
  await chmod(directory, 0o700);
  return {
    directory,
    configuration: {
      baseURL: "https://kodex.test",
      browser: "chromium" as const,
      journalPath: join(directory, "state.jsonl"),
      prefix: "fixture-lifecycle",
      resume: false,
      runTimeoutMs: 60000,
      syntheticSource: "synthetic",
      syntheticSourceSHA256: "5".repeat(64),
      versions,
    },
  };
}

test("fsync intent предшествует PASS и журнал не содержит исходник", async () => {
  const value = await fixture();
  const journal = await openLifecycleJournal(value.configuration);
  const sequence = await journal.intent("INTEGRATION_CREATE", "private source");
  expect(journal.pending()).toHaveLength(1);
  await journal.stop(
    sequence,
    "INTEGRATION_CREATE",
    "PASS",
    { ref: "cfg_fixture", version: 1, revision: 1 },
    201,
    '{"private":"source"}',
  );
  await journal.close();
  const raw = await readFile(value.configuration.journalPath, "utf8");
  expect(raw).not.toContain("private source");
  expect(raw).not.toContain('"private":"source"');
  const intent = JSON.parse(raw.trim().split("\n")[1] ?? "{}") as unknown;
  expect((intent as { type?: unknown }).type).toBe("intent");
  expect(journal.pending()).toEqual([]);
});

test("resume сохраняет UNKNOWN intent без повторного intent", async () => {
  const value = await fixture();
  const first = await openLifecycleJournal(value.configuration);
  await first.intent("PROMPT_PUBLISH", "publish");
  await first.close();
  const resumed = await openLifecycleJournal({
    ...value.configuration,
    resume: true,
  });
  expect(resumed.pending()).toHaveLength(1);
  await expect(resumed.intent("PROMPT_PUBLISH", "publish")).rejects.toThrow(
    "Unresolved",
  );
  await resumed.stop(1, "PROMPT_PUBLISH", "UNKNOWN");
  expect(resumed.pending()).toHaveLength(1);
  await resumed.stop(1, "PROMPT_PUBLISH", "PASS", {
    ref: "cfg_fixture",
    version: 2,
  });
  expect(resumed.pending()).toEqual([]);
  await resumed.close();
});

test("journal scope mismatch и небезопасный каталог закрыто отклоняются", async () => {
  const value = await fixture();
  const first = await openLifecycleJournal(value.configuration);
  await first.close();
  await expect(
    openLifecycleJournal({
      ...value.configuration,
      prefix: "other-prefix",
      resume: true,
    }),
  ).rejects.toThrow("scope mismatch");
  await chmod(value.directory, 0o755);
  await expect(
    openLifecycleJournal({
      ...value.configuration,
      journalPath: join(value.directory, "new.jsonl"),
    }),
  ).rejects.toThrow("private");
});

test("HTTP outcomes классифицируются без автоматического повтора", () => {
  expect(operationOutcome(undefined)).toBe("UNKNOWN");
  expect(operationOutcome(503)).toBe("UNKNOWN");
  expect(operationOutcome(412)).toBe("REJECTED");
  expect(operationOutcome(201)).toBe("PASS");
});

test("network guard разрешает только lifecycle UI commands и session transport", () => {
  expect(
    permittedLifecycleRequest("GET", "/api/v1/managed-configurations"),
  ).toBe(true);
  expect(
    permittedLifecycleRequest(
      "POST",
      "/api/v1/integration-definition-configurations/drafts",
    ),
  ).toBe(true);
  expect(
    permittedLifecycleRequest(
      "POST",
      "/api/v1/prompt-template-configurations/cfg_a/revisions/rev_a/impact-plans",
    ),
  ).toBe(true);
  expect(
    permittedLifecycleRequest(
      "POST",
      "/api/v1/integration-definition-configurations/copies",
    ),
  ).toBe(true);
  expect(
    permittedLifecycleRequest(
      "POST",
      "/api/v1/integration-definition-configurations/cfg_a/archive",
    ),
  ).toBe(true);
  expect(
    permittedLifecycleRequest(
      "POST",
      "/api/v1/integration-definition-configurations/cfg_a/revisions/rev_a/consumer-bindings",
    ),
  ).toBe(false);
  expect(
    permittedLifecycleRequest("POST", "/api/v1/integration-connections"),
  ).toBe(false);
  expect(permittedLifecycleRequest("POST", "/api/v1/runs")).toBe(false);
  expect(
    permittedLifecycleRequest("DELETE", "/api/v1/managed-configurations/cfg_a"),
  ).toBe(false);
});

test("check-only конфигурация не требует приватных runtime inputs", async () => {
  const configuration = await loadLifecycleConfiguration(
    {
      KODEX_E2E_CHECK_ONLY: "1",
      KODEX_E2E_SOURCE_REVISION: versions.harness,
      KODEX_E2E_API_REVISION: versions.api,
      KODEX_E2E_PWA_REVISION: versions.pwa,
      KODEX_E2E_SERVING_MANIFEST_SHA256: versions.servingManifestSHA256,
    },
    versions,
  );
  expect(configuration.prefix).toBe("check-only");
  expect(configuration.syntheticSourceSHA256).toMatch(/^[a-f0-9]{64}$/);
});

test("live-конфигурация связывает exact private manifest и tracked source", async () => {
  const value = await fixture();
  const manifestPath = join(value.directory, "serving.json");
  const sourcePath = join(value.directory, "synthetic.yaml");
  const manifest = Buffer.from('{"schemaVersion":1}\n');
  await writeFile(manifestPath, manifest, { mode: 0o600 });
  await writeFile(sourcePath, "origin: SHIPPED\n", { mode: 0o600 });
  const exactVersions = {
    ...versions,
    servingManifestSHA256: createHash("sha256").update(manifest).digest("hex"),
  };
  const configuration = await loadLifecycleConfiguration(
    {
      KODEX_E2E_BASE_URL: "https://kodex.test",
      KODEX_E2E_BROWSER: "webkit",
      KODEX_E2E_RESOURCE_PREFIX: "fixture-live",
      KODEX_E2E_RUN_TIMEOUT_MS: "60000",
      KODEX_E2E_CONFIGURATION_LIFECYCLE_CONFIRM:
        "RUN_CONFIGURATION_UI_LIFECYCLE",
      KODEX_E2E_CONFIGURATION_LIFECYCLE_STATE: join(
        value.directory,
        "live.jsonl",
      ),
      KODEX_E2E_CONFIGURATION_SYNTHETIC_SOURCE: sourcePath,
      KODEX_E2E_SERVING_MANIFEST: manifestPath,
    },
    exactVersions,
  );
  expect(configuration.browser).toBe("webkit");
  expect(configuration.syntheticSource).toBe("origin: SHIPPED\n");
  await expect(
    loadLifecycleConfiguration(
      {
        KODEX_E2E_BASE_URL: "https://kodex.test",
        KODEX_E2E_RESOURCE_PREFIX: "fixture-live",
        KODEX_E2E_CONFIGURATION_LIFECYCLE_CONFIRM:
          "RUN_CONFIGURATION_UI_LIFECYCLE",
        KODEX_E2E_CONFIGURATION_LIFECYCLE_STATE: join(
          value.directory,
          "live.jsonl",
        ),
        KODEX_E2E_CONFIGURATION_SYNTHETIC_SOURCE: sourcePath,
        KODEX_E2E_SERVING_MANIFEST: manifestPath,
      },
      versions,
    ),
  ).rejects.toThrow("digest mismatch");
});

test("повреждённый journal закрыто отклоняется при resume", async () => {
  const value = await fixture();
  await writeFile(value.configuration.journalPath, "{}\n", { mode: 0o600 });
  await expect(
    openLifecycleJournal({ ...value.configuration, resume: true }),
  ).rejects.toThrow();
});
