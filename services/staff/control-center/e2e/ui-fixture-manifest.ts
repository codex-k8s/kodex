import { createHash } from "node:crypto";
import { lstatSync, readFileSync, realpathSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";
import type { APIRequestContext } from "@playwright/test";

export const fixtureKinds = [
  "PROJECT",
  "AGENT",
  "WORKFLOW",
  "RUN",
  "ENVIRONMENT",
  "CONFIGURATION",
  "ASSISTANT",
  "VFS",
] as const;
export type FixtureKind = (typeof fixtureKinds)[number];
export interface FixturePin {
  slot: string;
  kind: FixtureKind;
  ref: string;
  version: number;
  projectRef?: string;
  configurationKind?:
    | "ROLE_IMAGE"
    | "PROMPT_TEMPLATE"
    | "INTEGRATION_DEFINITION";
  query?: string;
  path?: string;
  lifecycleState?: "ACTIVE" | "DELETED" | "ARCHIVED";
}
export interface FixtureManifest {
  schemaVersion: 1;
  fixtures: FixturePin[];
}
export class FixtureUnavailable extends Error {
  constructor(
    readonly code:
      | "SESSION"
      | "MISSING"
      | "FORBIDDEN"
      | "NOT_FOUND"
      | "VERSION_DRIFT"
      | "SCOPE_MISMATCH"
      | "EMPTY_PAGE"
      | "PAGE_BUDGET",
  ) {
    super("Readonly fixture unavailable");
  }
}
const refPattern = /^[a-zA-Z0-9_-]{1,100}$/;
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid fixture object");
  return value as Record<string, unknown>;
}
export function parseFixtureManifest(input: unknown): FixtureManifest {
  const value = object(input);
  if (
    Object.keys(value).sort().join(",") !== "fixtures,schemaVersion" ||
    value.schemaVersion !== 1 ||
    !Array.isArray(value.fixtures) ||
    !value.fixtures.length ||
    value.fixtures.length > 32
  )
    throw new Error("Invalid fixture manifest");
  const slots = new Set<string>();
  const fixtures = value.fixtures.map((raw) => {
    const item = object(raw);
    if (
      Object.keys(item).some(
        (key) =>
          ![
            "slot",
            "kind",
            "ref",
            "version",
            "projectRef",
            "configurationKind",
            "query",
            "path",
            "lifecycleState",
          ].includes(key),
      ) ||
      typeof item.slot !== "string" ||
      !/^[a-z][a-z0-9-]{0,47}$/.test(item.slot) ||
      slots.has(item.slot) ||
      !fixtureKinds.includes(item.kind as FixtureKind) ||
      typeof item.ref !== "string" ||
      !refPattern.test(item.ref) ||
      !Number.isSafeInteger(item.version) ||
      Number(item.version) < (item.kind === "VFS" ? 0 : 1) ||
      (item.projectRef !== undefined &&
        (typeof item.projectRef !== "string" ||
          !refPattern.test(item.projectRef))) ||
      (item.query !== undefined &&
        (typeof item.query !== "string" ||
          item.query.length < 2 ||
          item.query.length > 100 ||
          /[\r\n\0]/.test(item.query)))
    )
      throw new Error("Invalid fixture pin");
    if (
      ["AGENT", "WORKFLOW", "RUN", "VFS"].includes(String(item.kind)) &&
      !item.projectRef
    )
      throw new Error("Fixture project required");
    if (item.kind === "PROJECT" && item.projectRef !== undefined)
      throw new Error("Unexpected project scope");
    if (item.kind === "CONFIGURATION") {
      if (
        !["ROLE_IMAGE", "PROMPT_TEMPLATE", "INTEGRATION_DEFINITION"].includes(
          String(item.configurationKind),
        )
      )
        throw new Error("Fixture configuration kind required");
    } else if (item.configurationKind !== undefined)
      throw new Error("Unexpected configuration kind");
    if (item.kind === "VFS") {
      if (
        typeof item.path !== "string" ||
        !(
          item.path === `/projects/${String(item.projectRef)}` ||
          item.path.startsWith(`/projects/${String(item.projectRef)}/`)
        ) ||
        item.path.length > 512 ||
        item.path
          .split("/")
          .some((segment) => segment === "." || segment === "..") ||
        /[\\%?#\0]/.test(item.path) ||
        !["ACTIVE", "DELETED", "ARCHIVED"].includes(String(item.lifecycleState))
      )
        throw new Error("Invalid VFS fixture scope");
    } else if (item.path !== undefined || item.lifecycleState !== undefined)
      throw new Error("Unexpected VFS scope");
    slots.add(item.slot);
    return item as unknown as FixturePin;
  });
  return { schemaVersion: 1, fixtures };
}
export function loadFixtureManifest(path: string | undefined): {
  manifest?: FixtureManifest;
  sha256: string;
} {
  if (path === undefined) return { sha256: "" };
  if (!isAbsolute(path) || resolve(path) !== path)
    throw new Error("Private canonical fixture path required");
  const info = lstatSync(path);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    info.size > 65536 ||
    (info.mode & 0o077) !== 0 ||
    realpathSync(path) !== path
  )
    throw new Error("Private fixture file required");
  const raw = readFileSync(path);
  return {
    manifest: parseFixtureManifest(JSON.parse(raw.toString("utf8"))),
    sha256: createHash("sha256").update(raw).digest("hex"),
  };
}
export function fixturePath(pin: FixturePin): string {
  const paths = {
    PROJECT: "projects",
    AGENT: "agents",
    WORKFLOW: "workflows",
    RUN: "runs",
    ENVIRONMENT: "runtime-environments",
  } as const;
  if (pin.kind in paths)
    return `/api/v1/${paths[pin.kind as keyof typeof paths]}/${pin.ref}`;
  if (pin.kind === "CONFIGURATION")
    return `/api/v1/managed-configurations/${pin.ref}/revisions?pageSize=30`;
  if (pin.kind === "ASSISTANT")
    return (
      "/api/v1/assistant-conversations?" +
      new URLSearchParams({
        pageSize: "30",
        ...(pin.projectRef ? { projectRef: pin.projectRef } : {}),
      }).toString()
    );
  const query = new URLSearchParams({
    projectRef: pin.projectRef ?? "",
    path: pin.path ?? "",
    lifecycleState: pin.lifecycleState ?? "ACTIVE",
    pageSize: "30",
  });
  return `/api/v1/vfs/nodes?${query.toString()}`;
}
export function assertFixtureReadback(pin: FixturePin, raw: unknown): void {
  const value = object(raw);
  if (value.ref !== pin.ref) throw new FixtureUnavailable("SCOPE_MISMATCH");
  if (value.version !== pin.version)
    throw new FixtureUnavailable("VERSION_DRIFT");
  if (
    pin.kind !== "PROJECT" &&
    (value.projectRef ?? undefined) !== pin.projectRef
  )
    throw new FixtureUnavailable("SCOPE_MISMATCH");
  if (pin.kind === "CONFIGURATION" && value.kind !== pin.configurationKind)
    throw new FixtureUnavailable("SCOPE_MISMATCH");
  if (
    pin.kind === "VFS" &&
    (value.parentPath !== pin.path ||
      value.lifecycleState !== pin.lifecycleState)
  )
    throw new FixtureUnavailable("SCOPE_MISMATCH");
}
// Только текущая authenticated API boundary. Payload не назначает authority.
export async function validateFixture(
  request: APIRequestContext,
  pin: FixturePin,
): Promise<void> {
  let path = fixturePath(pin);
  const cursors = new Set<string>();
  for (let page = 0; page < 8; page++) {
    const response = await request.get(path, {
      timeout: 10000,
      failOnStatusCode: false,
    });
    let raw: unknown;
    try {
      if (response.status() === 401) throw new FixtureUnavailable("SESSION");
      if (response.status() === 403) throw new FixtureUnavailable("FORBIDDEN");
      if (response.status() === 404) throw new FixtureUnavailable("NOT_FOUND");
      if (response.status() !== 200)
        throw new Error("Fixture authoritative read failed");
      raw = await response.json();
    } finally {
      await response.dispose();
    }
    const value = object(raw);
    if (pin.kind === "CONFIGURATION") {
      assertFixtureReadback(pin, value.configuration);
      return;
    }
    if (!["ASSISTANT", "VFS"].includes(pin.kind)) {
      assertFixtureReadback(pin, value);
      return;
    }
    if (!Array.isArray(value.items)) throw new Error("Invalid fixture page");
    const selected = value.items.filter((item) => object(item).ref === pin.ref);
    if (selected.length > 1) throw new Error("Duplicate fixture response");
    if (selected.length) {
      assertFixtureReadback(pin, selected[0]);
      return;
    }
    const cursor = value.nextPageToken;
    if ((cursor === undefined && pin.kind === "ASSISTANT") || cursor === "")
      throw new FixtureUnavailable(
        value.items.length ? "NOT_FOUND" : "EMPTY_PAGE",
      );
    if (
      typeof cursor !== "string" ||
      cursor.length > 2048 ||
      cursors.has(cursor)
    )
      throw new Error("Invalid fixture cursor");
    cursors.add(cursor);
    const url = new URL(path, "https://fixture.invalid");
    url.searchParams.set("pageToken", cursor);
    path = url.pathname + url.search;
  }
  throw new FixtureUnavailable("PAGE_BUDGET");
}
