export interface VfsFolderLocation {
  path: string;
  name: string;
}

export function parseVfsTrail(value: unknown): VfsFolderLocation[] {
  if (typeof value !== "string" || value.length > 8192) return [];
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed) || parsed.length > 24) return [];
    const result: VfsFolderLocation[] = [];
    for (const item of parsed) {
      if (!item || typeof item !== "object") return [];
      const folder = item as Partial<VfsFolderLocation>;
      if (
        typeof folder.path !== "string" ||
        typeof folder.name !== "string" ||
        !folder.path.startsWith("/projects/") ||
        folder.path.length > 2048 ||
        !folder.name ||
        folder.name.length > 512 ||
        /[\p{Cc}\\]/u.test(folder.path) ||
        folder.path.split("/").some((part) => part === "." || part === "..") ||
        result.some((previous) => previous.path === folder.path)
      )
        return [];
      result.push({ path: folder.path, name: folder.name });
    }
    return result;
  } catch {
    return [];
  }
}

export function safeVfsReturn(
  value: unknown,
  projectRef: string,
): string | undefined {
  if (
    typeof value !== "string" ||
    value.length > 16384 ||
    !value.startsWith("/") ||
    value.startsWith("//")
  )
    return undefined;
  try {
    const url = new URL(value, "https://kodex.invalid");
    if (
      url.origin !== "https://kodex.invalid" ||
      url.hash ||
      !["/files", `/projects/${encodeURIComponent(projectRef)}/files`].includes(
        url.pathname,
      ) ||
      [...url.searchParams.keys()].some(
        (key) =>
          !["vfsTrail", "vfsQuery", "view", "vfsState", "vfsKinds"].includes(
            key,
          ),
      )
    )
      return undefined;
    const trail = url.searchParams.get("vfsTrail");
    if (
      new Set(url.searchParams.keys()).size !==
      [...url.searchParams.keys()].length
    )
      return undefined;
    const state = url.searchParams.get("vfsState");
    if (state && !vfsLifecycleStates.some((value) => value === state))
      return undefined;
    const kinds = url.searchParams.get("vfsKinds");
    if (
      kinds &&
      kinds.split(",").some((kind) => !vfsKinds.some((value) => value === kind))
    )
      return undefined;
    if (trail && !parseVfsTrail(trail).length) return undefined;
    if (url.searchParams.has("view") && url.searchParams.get("view") !== "vfs")
      return undefined;
    return `${url.pathname}${url.search}`;
  } catch {
    return undefined;
  }
}
import type { VfsKind } from "@/shared/api/generated/openapi/types.gen";

export const vfsKinds: VfsKind[] = [
  "DIRECTORY",
  "PROJECT",
  "AGENT",
  "WORKFLOW",
  "RUN",
  "INPUT",
  "RESULT",
  "SKILL",
  "MEMORY",
  "AUTOMATION",
  "ENVIRONMENT",
  "AVATAR",
];
export const vfsLifecycleStates = ["ACTIVE", "DELETED"] as const;
