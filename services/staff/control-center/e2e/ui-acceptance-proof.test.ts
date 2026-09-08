import { readonlyFormVariants } from "./ui-readonly-variant-ids";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, stat, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, test } from "vitest";
import {
  applicability,
  integrationPageShape,
  conditionFailure,
  UIConditionError,
  selectedVariants,
  targetedVariants,
  createJournal,
  permittedRequest,
  projectRefs,
  requirements,
  safeVariant,
  versionsFromEnvironment,
  type Variant,
} from "./ui-acceptance-proof";
const versions = {
  harness: "a".repeat(40),
  api: "b".repeat(40),
  pwa: "c".repeat(40),
  servingManifestSHA256: "d".repeat(64),
};
const variant: Variant = {
  id: "route-projects-ru-1440",
  requirements: ["MVP-UI-03", "MVP-UI-10"],
  status: "PASS",
  reason: "OBSERVED",
  locale: "ru",
  width: 1440,
  metrics: { overflow: 0 },
  timestampUTC: "2026-09-08T12:00:00.000Z",
};
const directories: string[] = [];
async function temporary() {
  const path = await mkdtemp(join(tmpdir(), "k1260-ui-"));
  await chmod(path, 0o700);
  directories.push(path);
  return path;
}
afterEach(async () => {
  await Promise.all(
    directories
      .splice(0)
      .map((path) => rm(path, { recursive: true, force: true })),
  );
});

test("полная applicability сохраняет 64 требования и partial PASS не закрывает ID", () => {
  const map = applicability([variant]);
  expect(requirements).toHaveLength(64);
  expect(new Set(requirements).size).toBe(64);
  expect(map).toHaveLength(64);
  expect(new Set(map.map((value) => value.fullRequirementStatus))).toEqual(
    new Set(["NOT RUN"]),
  );
  expect(
    map.find((value) => value.requirement === "MVP-UI-03")?.variants,
  ).toEqual([{ id: variant.id, status: "PASS" }]);
  expect(map.find((value) => value.requirement === "CFG-03")?.variants).toEqual(
    [],
  );
});
test("projection не сохраняет дополнительные DOM/cookie поля и отвергает произвольный payload в metrics", () => {
  const input = {
    ...variant,
    cookies: "sensitive",
    body: "sensitive",
    url: "sensitive",
  };
  expect(JSON.stringify(safeVariant(input))).not.toContain("sensitive");
  expect(() =>
    safeVariant({ ...variant, metrics: { value: "sensitive" } as never }),
  ).toThrow("metrics");
  expect(() =>
    safeVariant({ ...variant, metrics: { value: Infinity } }),
  ).toThrow("metrics");
  expect(() =>
    safeVariant({ ...variant, requirements: ["MVP-UI-99"] }),
  ).toThrow();
  expect(() => safeVariant({ ...variant, id: "route?cookie=value" })).toThrow();
});
test("переносимые версии и digest обязательны, project refs не принимают traversal и malformed page", () => {
  expect(
    versionsFromEnvironment({
      KODEX_E2E_SOURCE_REVISION: versions.harness,
      KODEX_E2E_API_REVISION: versions.api,
      KODEX_E2E_PWA_REVISION: versions.pwa,
      KODEX_E2E_SERVING_MANIFEST_SHA256: versions.servingManifestSHA256,
    }),
  ).toEqual(versions);
  expect(() => versionsFromEnvironment({})).toThrow();
  expect(
    projectRefs({
      items: [
        { ref: "prj_one", personalName: "discarded" },
        { ref: "prj_two" },
        { ref: "prj_three" },
      ],
    }),
  ).toEqual(["prj_one", "prj_two"]);
  for (const input of [
    null,
    { items: null },
    { items: [{ ref: "../secret" }] },
    { items: [{ ref: "x" }, { ref: "x" }] },
  ])
    expect(() => projectRefs(input)).toThrow();
});
test("граница запрещает Run/device/STT/publish/delete и разрешает exact opt-in project create", () => {
  for (const [method, path] of [
    ["POST", "/api/v1/runs"],
    ["POST", "/api/v1/session"],
    ["POST", "/api/v1/session/ticket/extra"],
    ["POST", "/api/v1/speech/transcriptions"],
    ["DELETE", "/api/v1/projects/prj_one"],
    ["POST", "/api/v1/provider-accounts/device/start"],
  ] as const)
    expect(permittedRequest(method, path, true)).toBe(false);
  expect(permittedRequest("POST", "/api/v1/projects", false)).toBe(false);
  expect(permittedRequest("POST", "/api/v1/projects", true)).toBe(true);
  expect(permittedRequest("PUT", "/api/v1/session", false)).toBe(true);
  expect(permittedRequest("POST", "/api/v1/session/ticket", false)).toBe(true);
  expect(permittedRequest("GET", "/api/v1/projects", false)).toBe(true);
});
test("journal сохраняет intent до receipt, mode0600, digest и не принимает повторный каталог", async () => {
  const parent = await temporary();
  const path = join(parent, "attempt");
  const journal = await createJournal(path, versions, "chromium");
  await journal.projectIntent(0);
  const initial = await readFile(journal.path, "utf8");
  expect(initial).toContain("fixture-intent");
  expect(initial).not.toContain("fixture-receipt");
  await journal.projectReceipt(0, "private-fixture-ref");
  await journal.variant(variant);
  await journal.close([variant]);
  const payload = await readFile(journal.path, "utf8");
  expect(payload).not.toContain("private-fixture-ref");
  expect(
    payload
      .trim()
      .split("\n")
      .map((line) => (JSON.parse(line) as { type: string }).type),
  ).toEqual([
    "metadata",
    "fixture-intent",
    "fixture-receipt",
    "variant",
    "applicability",
  ]);
  expect((await stat(journal.path)).mode & 0o777).toBe(0o600);
  expect(await readFile(join(path, "ui-acceptance-safe.sha256"), "utf8")).toBe(
    `${createHash("sha256").update(payload).digest("hex")}\n`,
  );
  await expect(createJournal(path, versions, "chromium")).rejects.toThrow();
  expect(await readFile(journal.path, "utf8")).toBe(payload);
});
test("journal отклоняет общий и symlink parent до записи", async () => {
  const parent = await temporary();
  await chmod(parent, 0o755);
  await expect(
    createJournal(join(parent, "public"), versions, "chromium"),
  ).rejects.toThrow("private");
  await chmod(parent, 0o700);
  const target = await temporary();
  const link = join(parent, "link");
  await symlink(target, link);
  await expect(
    createJournal(join(link, "child"), versions, "chromium"),
  ).rejects.toThrow("private");
});

test("закрытая диагностика сохраняет измерение и не извлекает текст ошибки", () => {
  const failure = conditionFailure(
    new UIConditionError("DOCUMENT_OVERFLOW", 560, 1),
  );
  expect(failure).toEqual({
    condition: "DOCUMENT_OVERFLOW",
    metrics: { measurementAvailable: true, actual: 560, expected: 1 },
  });
  const row = safeVariant({ ...variant, status: "FAIL", ...failure });
  expect(row.condition).toBe("DOCUMENT_OVERFLOW");
  expect(conditionFailure(new Error("cookie=secret"))).toEqual({
    condition: "UI_ACTION",
    metrics: { measurementAvailable: false },
  });
  expect(() =>
    safeVariant({ ...variant, condition: "cookie=secret" as never }),
  ).toThrow("condition");
  expect(
    conditionFailure(new UIConditionError("PAGE_HEADING", undefined, true))
      .metrics,
  ).toEqual({ measurementAvailable: false, expected: true });
});
test("targeted профиль допускает только восемь read-only variants без fixture writes", () => {
  expect(selectedVariants(undefined, "1")).toBeUndefined();
  const selected = selectedVariants(targetedVariants.join(","), "0");
  expect(selected?.size).toBe(8);
  expect(selected?.has("fixture-create-project-0")).toBe(false);
  for (const raw of [
    "",
    "fixture-create-project-0",
    "cookie=secret",
    "project-picker-keyboard-escape,project-picker-keyboard-escape",
  ])
    expect(() => selectedVariants(raw, "0")).toThrow("selection");
  expect(() => selectedVariants(targetedVariants[0], "1")).toThrow("selection");
});

test("integration shape сохраняет только наличие и тип terminal cursor", () => {
  const missing = integrationPageShape({
    items: [{ name: "secret-sentinel" }],
  });
  expect(missing).toMatchObject({
    connectionItemsArray: true,
    connectionCursorPresent: false,
    connectionCursorString: false,
  });
  expect(integrationPageShape({ items: [], nextPageToken: "" })).toMatchObject({
    connectionCursorPresent: true,
    connectionCursorString: true,
    connectionCursorEmpty: true,
  });
  expect(integrationPageShape({ items: [], nextPageToken: 1 })).toMatchObject({
    connectionCursorString: false,
  });
  expect(
    JSON.stringify(
      integrationPageShape({ items: [], nextPageToken: "secret-sentinel" }),
    ),
  ).not.toContain("secret-sentinel");
});

test("forms профиль имеет24 закрытых IDs и не разрешает create при выборе", () => {
  expect(readonlyFormVariants).toHaveLength(24);
  expect(new Set(readonlyFormVariants).size).toBe(24);
  expect(selectedVariants(readonlyFormVariants.join(","), "0")?.size).toBe(24);
  expect(() => selectedVariants(readonlyFormVariants[0], "1")).toThrow(
    "selection",
  );
  expect(() =>
    selectedVariants("configuration-create-editor-untrusted-ru-1440", "0"),
  ).toThrow("selection");
});
