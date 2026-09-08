import { describe, expect, it } from "vitest";
import {
  mkdtempSync,
  writeFileSync,
  chmodSync,
  symlinkSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  parseFixtureManifest,
  loadFixtureManifest,
  assertFixtureReadback,
  fixturePath,
  FixtureUnavailable,
  type FixturePin,
} from "./ui-fixture-manifest";
const project: FixturePin = {
  slot: "project-primary",
  kind: "PROJECT",
  ref: "project_fixture",
  version: 2,
};
const manifest = (pin: unknown = project) => ({
  schemaVersion: 1,
  fixtures: [pin],
});
describe("private exact existing fixture manifest", () => {
  it("rejects unknown authority fields, duplicates, traversal and invalid scope", () => {
    for (const pin of [
      { ...project, actor: "private-sentinel" },
      { ...project, version: 0 },
      { ...project, projectRef: "other" },
      { ...project, ref: "../other" },
      { ...project, kind: "AGENT" },
      { ...project, kind: "CONFIGURATION" },
      { ...project, path: "/projects/x" },
      {
        ...project,
        kind: "VFS",
        projectRef: "p",
        path: "/projects/other/x",
        lifecycleState: "ACTIVE",
      },
      {
        ...project,
        kind: "VFS",
        projectRef: "p",
        path: "/projects/p/../x",
        lifecycleState: "ACTIVE",
      },
    ])
      expect(() => parseFixtureManifest(manifest(pin))).toThrow();
    expect(() =>
      parseFixtureManifest({ schemaVersion: 1, fixtures: [project, project] }),
    ).toThrow();
  });
  it("keeps actual VFS version zero and exact project root", () => {
    const pin = {
      ...project,
      kind: "VFS",
      version: 0,
      projectRef: "p",
      path: "/projects/p",
      lifecycleState: "ACTIVE",
    };
    expect(parseFixtureManifest(manifest(pin)).fixtures[0]).toEqual(pin);
  });
  it("separates version, foreign project, configuration kind and lifecycle drift", () => {
    const pin: FixturePin = {
      slot: "configuration-primary",
      kind: "CONFIGURATION",
      configurationKind: "ROLE_IMAGE",
      ref: "c",
      version: 2,
      projectRef: "p",
    };
    expect(() =>
      assertFixtureReadback(pin, {
        ref: "c",
        version: 2,
        projectRef: "p",
        kind: "ROLE_IMAGE",
      }),
    ).not.toThrow();
    for (const raw of [
      { ref: "c", version: 3, projectRef: "p", kind: "ROLE_IMAGE" },
      { ref: "c", version: 2, projectRef: "other", kind: "ROLE_IMAGE" },
      { ref: "c", version: 2, projectRef: "p", kind: "PROMPT_TEMPLATE" },
    ])
      expect(() => assertFixtureReadback(pin, raw)).toThrow(FixtureUnavailable);
    expect(fixturePath(pin)).toBe(
      "/api/v1/managed-configurations/c/revisions?pageSize=30",
    );
  });
  it("accepts only regular private bounded files and never returns raw fixture as evidence", () => {
    const dir = mkdtempSync(join(tmpdir(), "uif-"));
    try {
      const file = join(dir, "fixture.json");
      writeFileSync(file, JSON.stringify(manifest()), { mode: 0o600 });
      expect(loadFixtureManifest(file).sha256).toMatch(/^[a-f0-9]{64}$/);
      chmodSync(file, 0o644);
      expect(() => loadFixtureManifest(file)).toThrow();
      chmodSync(file, 0o600);
      symlinkSync(file, join(dir, "link"));
      expect(() => loadFixtureManifest(join(dir, "link"))).toThrow();
      writeFileSync(file, " ".repeat(65537));
      expect(() => loadFixtureManifest(file)).toThrow();
    } finally {
      rmSync(dir, { recursive: true });
    }
  });
});
