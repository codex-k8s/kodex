import type {
  RoleImageArtifact,
  RoleImageArtifactTool,
  ImagePlatformToolInventory,
} from "@/shared/api/generated/openapi/types.gen";

const digest = /^[a-f0-9]{64}$/;
const imageDigest = /^sha256:[a-f0-9]{64}$/;
const executablePath =
  /^\/(?:usr\/local\/bin|usr\/bin|bin|usr\/local\/go\/bin)\/([A-Za-z0-9][A-Za-z0-9._+-]{0,159})$/;
const observationCount: ImagePlatformToolInventory["tools"]["length"] = 50;
const observationStatuses = new Set<string>([
  "VERIFIED",
  "MISSING",
  "PROBE_FAILED",
]);

// UI использует owner-verified public evidence, не проверяет подпись заново
// и не назначает capability. Декларации recipe никогда не являются fallback.
export function verifiedImageInventoryAvailable(
  artifact?: RoleImageArtifact,
): boolean {
  if (!artifact) return false;
  const inventory = artifact.verifiedToolInventory as
    | RoleImageArtifact["verifiedToolInventory"]
    | undefined;
  if (
    !inventory ||
    inventory.status !== "VERIFIED" ||
    !digest.test(inventory.sha256) ||
    inventory.imageDigest !==
      `sha256:${artifact.manifestDigest.replace(/^sha256:/, "")}` ||
    !imageDigest.test(inventory.imageDigest) ||
    inventory.provenanceSha256 !== artifact.provenanceSha256 ||
    !digest.test(inventory.provenanceSha256) ||
    !Array.isArray(inventory.platforms) ||
    inventory.platforms.length < 1 ||
    inventory.platforms.length > 2
  )
    return false;
  const platforms = new Set<string>();
  for (const platform of inventory.platforms) {
    if (
      !["linux/amd64", "linux/arm64"].includes(platform.platform) ||
      platforms.has(platform.platform) ||
      !imageDigest.test(platform.platformDigest) ||
      !digest.test(platform.manifestSha256) ||
      !Array.isArray(platform.tools) ||
      Array.from(platform.tools).length !== observationCount
    )
      return false;
    platforms.add(platform.platform);
    const names = new Set<string>();
    const commands = new Set<string>();
    for (const tool of platform.tools) {
      if (
        names.has(tool.name) ||
        typeof tool.required !== "boolean" ||
        !observationStatuses.has(tool.status)
      )
        return false;
      names.add(tool.name);
      if (tool.status === "MISSING") {
        if (tool.path || tool.version || tool.sha256) return false;
        continue;
      }
      const command =
        tool.path === "/usr/lib/chromium/chromium"
          ? "chromium"
          : executablePath.exec(tool.path)?.[1];
      if (!command || commands.has(command) || !digest.test(tool.sha256))
        return false;
      commands.add(command);
      if (tool.status === "VERIFIED") {
        if (
          !/^v?[0-9]+(?:\.[0-9]+){0,3}(?:[-+][A-Za-z0-9.-]{1,48})?$/.test(
            tool.version,
          ) ||
          tool.version.length > 80
        )
          return false;
      } else if (tool.version !== "") return false;
    }
  }
  return true;
}

// Без target-platform pin UI предлагает только пересечение всех подписанных
// platform observations. Команда берётся из observed executable basename: rg/tsc.
export function verifiedImageTools(
  artifact?: RoleImageArtifact,
): RoleImageArtifactTool[] {
  if (!verifiedImageInventoryAvailable(artifact) || !artifact) return [];
  const platforms = artifact.verifiedToolInventory.platforms.map(
    (platform) =>
      new Map(
        platform.tools
          .filter((tool) => tool.status === "VERIFIED")
          .map((tool) => [tool.path.split("/").at(-1) ?? "", tool]),
      ),
  );
  const first = platforms[0];
  if (!first) return [];
  return [...first]
    .filter(([command]) => platforms.every((platform) => platform.has(command)))
    .map(([command, tool]) => ({
      name: command,
      version: [
        ...new Set(
          platforms.map(
            (platform) => platform.get(command)?.version ?? tool.version,
          ),
        ),
      ].join(" / "),
    }));
}
