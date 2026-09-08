import type { ReadNetworkCorrelator } from "./ui-read-network";
import { populatedVariants } from "./ui-populated-variants";
import { readonlyFormVariants } from "./ui-readonly-variant-ids";
import { createHash } from "node:crypto";
import { lstat, mkdir, open, realpath } from "node:fs/promises";
import { dirname, isAbsolute, join, resolve } from "node:path";

export const requirements = [
  ...Array.from(
    { length: 61 },
    (_, i) => `MVP-UI-${String(i + 1).padStart(2, "0")}`,
  ),
  "CFG-01",
  "CFG-02",
  "CFG-03",
];
export const widths = [1280, 1440, 1920, 2560, 2900, 390, 768] as const;
export type Outcome = "PASS" | "FAIL" | "NOT RUN";
export type Reason =
  | "OBSERVED"
  | "UI_ASSERTION_FAILED"
  | "DEPENDENCY_UNAVAILABLE"
  | "FIXTURE_UNAVAILABLE"
  | "BUDGET_EXHAUSTED"
  | "OUTSIDE_PROFILE"
  | "UNKNOWN_OUTCOME";
export const conditions = [
  "HTTP_STATUS",
  "ROUTE_MISMATCH",
  "APP_SHELL",
  "PAGE_HEADING",
  "DOCUMENT_OVERFLOW",
  "HEADER_HEIGHT",
  "VISIBLE_ALERT",
  "UNTRANSLATED",
  "API_READINESS",
  "HTTP_ERRORS",
  "PAGE_ERRORS",
  "NETWORK_ERRORS",
  "CONSOLE_ERRORS",
  "BLOCKED_WRITES",
  "SELECTOR_OPEN",
  "SELECTOR_FOCUS",
  "SELECTOR_ESCAPE",
  "SELECTOR_RETURN_FOCUS",
  "ASSISTANT_OPEN",
  "ASSISTANT_VISIBLE",
  "ASSISTANT_FOCUS",
  "ASSISTANT_ESCAPE",
  "UI_ACTION",
  "CLEANUP",
] as const;
export type Condition = (typeof conditions)[number];
export class UIConditionError extends Error {
  constructor(
    readonly condition: Condition,
    readonly actual?: number | boolean,
    readonly expected?: number | boolean,
  ) {
    super("UI condition failed");
  }
}
export function conditionFailure(
  error: unknown,
): Pick<Variant, "condition" | "metrics"> {
  if (!(error instanceof UIConditionError))
    return { condition: "UI_ACTION", metrics: { measurementAvailable: false } };
  return {
    condition: error.condition,
    metrics: {
      measurementAvailable: error.actual !== undefined,
      ...(error.actual === undefined ? {} : { actual: error.actual }),
      ...(error.expected === undefined ? {} : { expected: error.expected }),
    },
  };
}
export const targetedVariants = [
  "route-integrations-ru-1440",
  "route-integrations-ru-390",
  "route-integrations-en-1440",
  "route-integrations-en-390",
  "project-picker-keyboard-escape",
  "assistant-history-shell-1440",
  "assistant-history-shell-768",
  "assistant-history-shell-390",
] as const;
export function selectedVariants(
  raw: string | undefined,
  createMode: string,
): Set<string> | undefined {
  if (raw === undefined) return undefined;
  const values = raw.split(",");
  if (
    createMode !== "0" ||
    !values.length ||
    new Set(values).size !== values.length ||
    values.some(
      (value) =>
        !targetedVariants.includes(
          value as (typeof targetedVariants)[number],
        ) &&
        !readonlyFormVariants.includes(value) &&
        !populatedVariants.includes(value),
    )
  )
    throw new Error("Invalid read-only UI variant selection");
  return new Set(values);
}
export interface Variant {
  id: string;
  requirements: readonly string[];
  status: Outcome;
  reason: Reason;
  condition?: Condition;
  locale: "ru" | "en";
  width: number;
  metrics: Record<string, number | boolean>;
  timestampUTC: string;
}
export interface Versions {
  harness: string;
  api: string;
  pwa: string;
  servingManifestSHA256: string;
}
export function versionsFromEnvironment(env: NodeJS.ProcessEnv): Versions {
  const values = {
    harness: env.KODEX_E2E_SOURCE_REVISION ?? "",
    api: env.KODEX_E2E_API_REVISION ?? "",
    pwa: env.KODEX_E2E_PWA_REVISION ?? "",
    servingManifestSHA256: env.KODEX_E2E_SERVING_MANIFEST_SHA256 ?? "",
  };
  if (
    ![values.harness, values.api, values.pwa].every((v) =>
      /^[a-f0-9]{40}$/.test(v),
    ) ||
    !/^[a-f0-9]{64}$/.test(values.servingManifestSHA256)
  )
    throw new Error(
      "UI proof requires exact component revisions and manifest digest",
    );
  return values;
}
export function safeVariant(variant: Variant): Variant {
  if (
    !/^[a-z][a-z0-9-]{0,95}$/.test(variant.id) ||
    !variant.requirements.length ||
    !variant.requirements.every((id) => requirements.includes(id))
  )
    throw new Error("Invalid UI proof variant");
  if (
    !["PASS", "FAIL", "NOT RUN"].includes(variant.status) ||
    ![
      "OBSERVED",
      "UI_ASSERTION_FAILED",
      "DEPENDENCY_UNAVAILABLE",
      "FIXTURE_UNAVAILABLE",
      "BUDGET_EXHAUSTED",
      "OUTSIDE_PROFILE",
      "UNKNOWN_OUTCOME",
    ].includes(variant.reason)
  )
    throw new Error("Invalid UI proof outcome");
  if (
    !["ru", "en"].includes(variant.locale) ||
    !widths.includes(variant.width as (typeof widths)[number]) ||
    !Number.isFinite(Date.parse(variant.timestampUTC))
  )
    throw new Error("Invalid UI proof dimensions");
  if (
    Object.entries(variant.metrics).some(
      ([key, value]) =>
        !/^[a-z][a-zA-Z0-9]{0,39}$/.test(key) ||
        !(
          typeof value === "boolean" ||
          (typeof value === "number" && Number.isFinite(value))
        ),
    )
  )
    throw new Error("Invalid UI proof metrics");
  if (
    variant.condition !== undefined &&
    !conditions.includes(variant.condition)
  )
    throw new Error("Invalid UI proof condition");
  // Только закрытые поля: случайно переданный DOM, URL или response отбрасывается.
  return {
    id: variant.id,
    requirements: [...variant.requirements],
    status: variant.status,
    reason: variant.reason,
    ...(variant.condition === undefined
      ? {}
      : { condition: variant.condition }),
    locale: variant.locale,
    width: variant.width,
    metrics: { ...variant.metrics },
    timestampUTC: variant.timestampUTC,
  };
}
export function applicability(variants: readonly Variant[]) {
  return requirements.map((requirement) => ({
    requirement,
    fullRequirementStatus: "NOT RUN" as const,
    variants: variants
      .filter((v) => v.requirements.includes(requirement))
      .map((v) => ({ id: v.id, status: v.status })),
    remaining: "MUTATION_RUNTIME_PROVIDER_AND_REQUIRED_VARIANTS",
  }));
}
export function projectRefs(value: unknown): string[] {
  if (
    !value ||
    typeof value !== "object" ||
    !Array.isArray((value as { items?: unknown }).items)
  )
    throw new Error("Invalid project fixture page");
  const result = (value as { items: unknown[] }).items
    .slice(0, 2)
    .map((item) => {
      const ref =
        item && typeof item === "object"
          ? (item as { ref?: unknown }).ref
          : undefined;
      if (typeof ref !== "string" || !/^[a-zA-Z0-9_-]{1,100}$/.test(ref))
        throw new Error("Invalid project fixture reference");
      return ref;
    });
  if (new Set(result).size !== result.length)
    throw new Error("Duplicate project fixture reference");
  return result;
}
export function permittedRequest(
  method: string,
  pathname: string,
  creatingProject: boolean,
  body?: unknown,
): boolean {
  return (
    ["GET", "HEAD", "OPTIONS"].includes(method) ||
    (pathname === "/api/v1/session" && method === "PUT") ||
    (pathname === "/api/v1/session/ticket" && method === "POST") ||
    (method === "POST" &&
      pathname === "/api/v1/administration/access/effective-access/query" &&
      roleImagePermissionRead(body)) ||
    (creatingProject && pathname === "/api/v1/projects" && method === "POST")
  );
}
function roleImagePermissionRead(body: unknown): boolean {
  if (!body || typeof body !== "object" || Array.isArray(body)) return false;
  const value = body as Record<string, unknown>;
  if (Object.keys(value).sort().join(",") !== "permissionKeys,target")
    return false;
  const permissions = [
    "image.build",
    "image.source.manage",
    "image.source.view",
  ];
  if (
    !Array.isArray(value.permissionKeys) ||
    ![2, 3].includes(value.permissionKeys.length) ||
    !value.permissionKeys.every((key) => typeof key === "string") ||
    ![permissions.join(","), permissions.slice(1).join(",")].includes(
      [...value.permissionKeys].sort().join(","),
    )
  )
    return false;
  if (
    !value.target ||
    typeof value.target !== "object" ||
    Array.isArray(value.target)
  )
    return false;
  const target = value.target as Record<string, unknown>;
  return (
    (target.kind === "ORGANIZATION" &&
      Object.keys(target).join(",") === "kind") ||
    (target.kind === "PROJECT" &&
      Object.keys(target).sort().join(",") === "kind,projectRef" &&
      typeof target.projectRef === "string" &&
      /^[a-zA-Z0-9_-]{1,100}$/.test(target.projectRef))
  );
}
export async function createJournal(
  rawPath: string,
  versions: Versions,
  browser: string,
  fixtureManifestSHA256 = "",
) {
  if (
    !isAbsolute(rawPath) ||
    rawPath !== resolve(rawPath) ||
    !["chromium", "firefox", "webkit"].includes(browser) ||
    !/^(?:[a-f0-9]{64})?$/.test(fixtureManifestSHA256)
  )
    throw new Error("Invalid UI evidence configuration");
  const parent = dirname(resolve(rawPath));
  const info = await lstat(parent);
  if (
    !info.isDirectory() ||
    info.isSymbolicLink() ||
    (info.mode & 0o077) !== 0 ||
    (await realpath(parent)) !== parent
  )
    throw new Error("UI evidence parent must be private and canonical");
  // Новый каталог служит guard повторного intent; Playwright его не удаляет.
  await mkdir(rawPath, { mode: 0o700 });
  const path = join(rawPath, "ui-acceptance-safe.jsonl");
  const file = await open(path, "wx", 0o600);
  const digest = createHash("sha256");
  const append = async (value: unknown) => {
    const line = `${JSON.stringify(value)}\n`;
    if (Buffer.byteLength(line) > 65_536)
      throw new Error("UI evidence record exceeds size limit");
    await file.writeFile(line);
    await file.sync();
    digest.update(line);
  };
  await append({
    schemaVersion: 1,
    type: "metadata",
    sourceRole: "harness",
    versions,
    browser,
    fixtureManifestSHA256,
    timestampUTC: new Date().toISOString(),
  });
  return {
    path,
    variant: async (value: Variant) =>
      append({
        type: "variant",
        browser,
        fixtureManifestSHA256,
        ...safeVariant(value),
      }),
    network: async (value: ReadNetworkCorrelator<object>) =>
      append({
        type: "read-network",
        timestampUTC: new Date().toISOString(),
        browser,
        fixtureManifestSHA256,
        summary: value.snapshot(),
        diagnostics: value.safeDiagnostics(),
      }),
    projectIntent: async (slot: 0 | 1) =>
      append({
        type: "fixture-intent",
        operation: "PROJECT_CREATE",
        slot,
        timestampUTC: new Date().toISOString(),
      }),
    projectReceipt: async (slot: 0 | 1, ref: string) =>
      append({
        type: "fixture-receipt",
        operation: "PROJECT_CREATE",
        slot,
        refSHA256: createHash("sha256").update(ref).digest("hex"),
        timestampUTC: new Date().toISOString(),
      }),
    close: async (variants: readonly Variant[]) => {
      try {
        await append({ type: "applicability", items: applicability(variants) });
      } finally {
        await file.close();
      }
      const hash = await open(
        join(rawPath, "ui-acceptance-safe.sha256"),
        "wx",
        0o600,
      );
      try {
        await hash.writeFile(`${digest.digest("hex")}\n`);
        await hash.sync();
      } finally {
        await hash.close();
      }
    },
  };
}

export function integrationPageShape(
  value: unknown,
): Record<string, boolean | number> {
  if (!value || typeof value !== "object")
    return { connectionShapeObserved: true, connectionObject: false };
  const page = value as Record<string, unknown>;
  return {
    connectionShapeObserved: true,
    connectionObject: true,
    connectionItemsArray: Array.isArray(page.items),
    connectionCursorPresent: Object.hasOwn(page, "nextPageToken"),
    connectionCursorString: typeof page.nextPageToken === "string",
    connectionCursorEmpty: page.nextPageToken === "",
  };
}
