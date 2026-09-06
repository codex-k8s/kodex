import { describe, expect, it } from "vitest";
import {
  importFiles,
  skillPathBytes,
  validSkillPath,
  checkImportedArtifact,
  validSkillSpecification,
} from "./skill-import";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
describe("Skill import boundary", () => {
  it("требует structural manifest уже для save, но не присваивает scan/review", () => {
    const specification = {
      name: "Skill",
      description: "",
      files: [
        { path: "SKILL.md", artifactRef: "artifact", artifactRevision: 3 },
      ],
    };
    expect(validSkillSpecification(specification)).toBe(true);
    expect(validSkillSpecification({ ...specification, files: [] })).toBe(
      false,
    );
    expect(
      validSkillSpecification({
        ...specification,
        description: "я".repeat(2001),
      }),
    ).toBe(false);
    expect(
      validSkillSpecification({
        ...specification,
        description: String.fromCodePoint(0x1d11e).repeat(2000),
      }),
    ).toBe(true);
    expect(
      validSkillSpecification({
        ...specification,
        files: [
          ...specification.files,
          { path: "skill.md", artifactRef: "other", artifactRevision: 1 },
        ],
      }),
    ).toBe(false);
  });
  it("ограничивает upload 32 MiB на файл, 64 MiB на очередь и 256 KiB на manifest", () => {
    const file = new File([], "image.png");
    Object.defineProperty(file, "size", { value: 32 * 1024 * 1024 + 1 });
    expect(() => importFiles([file], [])).toThrow();
    const small = new File(["abc"], "data.txt");
    expect(() => importFiles([small], [], 64 * 1024 * 1024)).toThrow();
    const manifest = new File([], "SKILL.md");
    Object.defineProperty(manifest, "size", { value: 256 * 1024 + 1 });
    expect(() => importFiles([manifest], [])).toThrow();
  });
  it.each([
    "SKILL.md",
    "references/context.md",
    "assets/example.png",
    "data.json",
  ])("принимает разрешенный путь %s", (path) => {
    expect(validSkillPath(path)).toBe(true);
  });
  it.each([
    "skill.md",
    "/SKILL.md",
    "../SKILL.md",
    "refs/../SKILL.md",
    "a//x.md",
    "a/ x.md",
    ".hidden.md",
    "scripts/run.py",
    "a\\x.md",
    "a:x.md",
    "a\0.md",
  ])("отклоняет путь %s", (path) => {
    expect(validSkillPath(path)).toBe(false);
  });
  it("считает UTF-8 bytes, а не code units", () => {
    expect(skillPathBytes("я".repeat(118) + ".md")).toBe(239);
    expect(validSkillPath("я".repeat(118) + ".md")).toBe(true);
    expect(validSkillPath("я".repeat(119) + ".md")).toBe(false);
  });
  it("сохраняет относительную структуру directory и отклоняет дубли", () => {
    const file = new File(["data"], "context.md");
    Object.defineProperty(file, "webkitRelativePath", {
      value: "bundle/references/context.md",
    });
    expect(importFiles([file], ["SKILL.md"])[0]?.path).toBe(
      "references/context.md",
    );
    expect(() => importFiles([file], ["REFERENCES/CONTEXT.MD"])).toThrow();
    expect(() =>
      importFiles(
        [new File([""], "SKILL.md")],
        Array.from({ length: 128 }, (_, index) => `${String(index)}.md`),
      ),
    ).toThrow();
  });
  it("не принимает измененную revision и чужой scope после scan refresh", () => {
    const artifact = {
      ref: "artifact",
      revision: 3,
      projectRef: "project",
      lifecycleState: "ACTIVE",
      scanState: "PENDING",
    } as Artifact;
    expect(checkImportedArtifact(artifact, "project", "artifact", 3)).toBe(
      artifact,
    );
    expect(() => checkImportedArtifact(artifact, "foreign")).toThrow();
    expect(() =>
      checkImportedArtifact(artifact, "project", "artifact", 2),
    ).toThrow();
  });
});
