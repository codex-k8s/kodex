import { describe, expect, it } from "vitest";
import { parseVfsTrail, safeVfsReturn } from "./vfs-location";

describe("VFS navigation location", () => {
  it("восстанавливает только ограниченный projection trail без authority", () => {
    const trail = [
      { path: "/projects/project_one", name: "Проект" },
      { path: "/projects/project_one/files", name: "Файлы" },
    ];
    expect(parseVfsTrail(JSON.stringify(trail))).toEqual(trail);
    expect(parseVfsTrail('{"path":"/projects/project_one"}')).toEqual([]);
    expect(
      parseVfsTrail(
        JSON.stringify([
          ...trail,
          { path: "/projects/../other", name: "Ошибка" },
        ]),
      ),
    ).toEqual([]);
    expect(parseVfsTrail(JSON.stringify([...trail, trail[0]]))).toEqual([]);
  });
  it("разрешает возврат только в глобальный VFS или файлы текущего проекта", () => {
    expect(safeVfsReturn("/files?vfsQuery=report", "one")).toBe(
      "/files?vfsQuery=report",
    );
    expect(safeVfsReturn("/projects/one/files", "one")).toBe(
      "/projects/one/files",
    );
    for (const value of [
      "https://external.invalid",
      "//external.invalid/files",
      "/projects/two/files",
      "/files?returnTo=/admin",
      "/files#other",
      "/files?vfsTrail=invalid",
      "/\\external.invalid/files",
    ])
      expect(safeVfsReturn(value, "one")).toBeUndefined();
  });
});
