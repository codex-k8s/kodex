import { describe, expect, it } from "vitest";
import type {
  RoleImageArtifact,
  ImageToolObservation,
} from "@/shared/api/generated/openapi/types.gen";
import {
  verifiedInventoryFixture,
  unavailableInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import {
  verifiedImageInventoryAvailable,
  verifiedImageTools,
} from "./verified-image-tools";

function artifact(): RoleImageArtifact {
  return {
    scopeKind: "ORGANIZATION",
    organizationRef: "org_fixture",
    projectRef: "",
    ref: "artifact_fixture",
    version: 1,
    recipeRef: "recipe_fixture",
    recipeGeneration: 1,
    buildRef: "build_fixture",
    manifestDigest: `sha256:${"a".repeat(64)}`,
    provenanceSha256: "b".repeat(64),
    admissionVerdict: "ACCEPTED",
    promotionState: "PROMOTED",
    promotionRequested: true,
    declaredTools: [{ name: "not_observed", version: "9.0" }],
    verifiedToolInventory: verifiedInventoryFixture(undefined, undefined, [
      "git",
      "ripgrep",
      "typescript",
    ]),
  };
}
describe("owner-verified inventory для выбора команд", () => {
  it("использует только наблюдаемые команды и реальные basename rg/tsc, не декларации", () => {
    const value = artifact();
    expect(verifiedImageInventoryAvailable(value)).toBe(true);
    expect(verifiedImageTools(value)).toEqual([
      { name: "git", version: "1.2.3" },
      { name: "rg", version: "1.2.3" },
      { name: "tsc", version: "1.2.3" },
    ]);
    expect(value.verifiedToolInventory.platforms[0]?.tools).toHaveLength(50);
  });
  it("не подменяет отсутствующее доказательство декларацией", () => {
    const value = artifact();
    value.verifiedToolInventory = unavailableInventoryFixture();
    expect(verifiedImageInventoryAvailable(value)).toBe(false);
    expect(verifiedImageTools(value)).toEqual([]);
    expect(verifiedImageTools()).toEqual([]);
  });
  it("пересекает VERIFIED платформы, допускает разные хеши архитектур и не разрешает PROBE_FAILED", () => {
    const value = artifact();
    const second = structuredClone(value.verifiedToolInventory.platforms[0]);
    if (!second) throw new Error("Missing synthetic platform");
    second.platform = "linux/arm64";
    const git = second.tools.find((tool) => tool.name === "git");
    const rg = second.tools.find((tool) => tool.name === "ripgrep");
    if (!git || !rg) throw new Error("Missing synthetic observations");
    git.sha256 = "f".repeat(64);
    git.version = "1.2.4";
    rg.status = "PROBE_FAILED";
    rg.version = "";
    value.verifiedToolInventory.platforms.push(second);
    expect(verifiedImageTools(value)).toEqual([
      { name: "git", version: "1.2.3 / 1.2.4" },
      { name: "tsc", version: "1.2.3" },
    ]);
  });
  it.each([
    (value: RoleImageArtifact) => {
      value.verifiedToolInventory.imageDigest = `sha256:${"f".repeat(64)}`;
    },
    (value: RoleImageArtifact) => {
      value.verifiedToolInventory.provenanceSha256 = "f".repeat(64);
    },
    (value: RoleImageArtifact) => {
      value.verifiedToolInventory.sha256 = "";
    },
    (value: RoleImageArtifact) => {
      value.verifiedToolInventory.platforms = [];
    },
    (value: RoleImageArtifact) => {
      const first = value.verifiedToolInventory.platforms[0];
      if (first)
        value.verifiedToolInventory.platforms.push(structuredClone(first));
    },
    (value: RoleImageArtifact) => {
      value.verifiedToolInventory.platforms[0]?.tools.pop();
    },
    (value: RoleImageArtifact) => {
      const tool = value.verifiedToolInventory.platforms[0]?.tools[2];
      if (tool) tool.path = "/private/credential/git";
    },
    (value: RoleImageArtifact) => {
      const tool = value.verifiedToolInventory.platforms[0]?.tools[2];
      if (tool) tool.sha256 = "";
    },
    (value: RoleImageArtifact) => {
      const tool = value.verifiedToolInventory.platforms[0]?.tools[1];
      if (tool) tool.version = "private unexpected value";
    },
    (value: RoleImageArtifact) => {
      const tool = value.verifiedToolInventory.platforms[0]?.tools[2];
      if (tool) tool.status = "UNKNOWN" as ImageToolObservation["status"];
    },
  ])("закрывает повреждённый либо несвязанный snapshot %i", (mutate) => {
    const value = artifact();
    mutate(value);
    expect(verifiedImageInventoryAvailable(value)).toBe(false);
    expect(verifiedImageTools(value)).toEqual([]);
  });
});
