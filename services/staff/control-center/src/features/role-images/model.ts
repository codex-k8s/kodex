import type {
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

export interface DockerfileToken {
  text: string;
  tone: "plain" | "comment" | "instruction" | "argument" | "variable";
}

export interface RoleImageApiGap {
  key:
    | "dockerfile"
    | "revisions"
    | "promotion"
    | "evidence"
    | "executables"
    | "environment-links";
  contract: string;
}

export const roleImageApiGaps: readonly RoleImageApiGap[] = [
  {
    key: "dockerfile",
    contract:
      "RoleImageRecipeCreateInput и RoleImageRecipeUpdateInput не содержат Dockerfile или ссылку на его immutable revision.",
  },
  {
    key: "revisions",
    contract:
      "RoleImageRecipeDetail возвращает только текущий generation и builds; API списка и чтения immutable image revisions отсутствует.",
  },
  {
    key: "promotion",
    contract:
      "Публичная команда promotion отсутствует; API возвращает только promotedImageReady.",
  },
  {
    key: "evidence",
    contract:
      "RoleImageBuild не возвращает digest, SBOM, provenance, подпись или ссылку на build log.",
  },
  {
    key: "executables",
    contract:
      "Публичный API инвентаризации обнаруженных и проверенных executable отсутствует.",
  },
  {
    key: "environment-links",
    contract:
      "Публичный API обратных ссылок Runtime Environment на exact promoted image revision отсутствует.",
  },
] as const;

const buildStageOrder: Record<RoleImageBuild["stage"], number> = {
  QUEUED: 0,
  MATERIALIZATION: 1,
  CONTEXT_VALIDATION: 2,
  BASE_PULL: 3,
  SOLVING: 4,
  INSTALLATION: 5,
  TRUSTED_RUNTIME_FINALIZATION: 6,
  STAGING_PUSH: 7,
  PROVENANCE: 8,
  COMPLETED: 9,
  FAILED: 9,
  CANCELLED: 9,
  EXPIRED: 9,
  DEAD_LETTER: 9,
};

export function latestBuild(
  builds: readonly RoleImageBuild[],
): RoleImageBuild | undefined {
  return [...builds].sort((left, right) => {
    const time = Date.parse(right.updatedAt) - Date.parse(left.updatedAt);
    if (time !== 0) return time;
    if (right.attempt !== left.attempt) return right.attempt - left.attempt;
    return buildStageOrder[right.stage] - buildStageOrder[left.stage];
  })[0];
}

export function buildIsTerminal(build: RoleImageBuild): boolean {
  return [
    "COMPLETED",
    "FAILED",
    "CANCELLED",
    "EXPIRED",
    "DEAD_LETTER",
  ].includes(build.stage);
}

export function buildIsActive(build: RoleImageBuild): boolean {
  return !buildIsTerminal(build);
}

export function canRequestBuild(recipe: RoleImageRecipe): boolean {
  return (
    recipe.state === "ACTIVE" && recipe.nextActions.includes("REQUEST_BUILD")
  );
}

export function roleImageState(
  recipe: RoleImageRecipe,
  build?: RoleImageBuild,
): RoleImageBuild["stage"] | RoleImageRecipe["state"] | "PROMOTED" {
  if (recipe.state === "ARCHIVED") return "ARCHIVED";
  if (build && build.stage !== "COMPLETED") return build.stage;
  if (recipe.promotedImageReady) return "PROMOTED";
  return build?.stage ?? recipe.state;
}

const dockerfileInstruction = /^([A-Z][A-Z0-9_-]*)(\s+.*)?$/;
const dockerfileVariable =
  /(\$\{[A-Za-z_][A-Za-z0-9_]*\}|\$[A-Za-z_][A-Za-z0-9_]*)/g;

function tokenizeArgument(value: string): DockerfileToken[] {
  const tokens: DockerfileToken[] = [];
  let cursor = 0;
  for (const match of value.matchAll(dockerfileVariable)) {
    const index = match.index ?? 0;
    if (index > cursor)
      tokens.push({ text: value.slice(cursor, index), tone: "argument" });
    tokens.push({ text: match[0], tone: "variable" });
    cursor = index + match[0].length;
  }
  if (cursor < value.length)
    tokens.push({ text: value.slice(cursor), tone: "argument" });
  return tokens.length ? tokens : [{ text: value, tone: "argument" }];
}

export function tokenizeDockerfileLine(line: string): DockerfileToken[] {
  const trimmed = line.trimStart();
  if (!trimmed) return [{ text: line, tone: "plain" }];
  if (trimmed.startsWith("#")) return [{ text: line, tone: "comment" }];
  const indentation = line.slice(0, line.length - trimmed.length);
  const match = trimmed.match(dockerfileInstruction);
  if (!match) return [{ text: line, tone: "plain" }];
  const instruction = match[1] ?? "";
  const argument = match[2] ?? "";
  return [
    ...(indentation
      ? ([{ text: indentation, tone: "plain" }] satisfies DockerfileToken[])
      : []),
    { text: instruction, tone: "instruction" },
    ...tokenizeArgument(argument),
  ];
}

export function validateDockerfile(value: string): string[] {
  const normalized = value.replace(/\r\n?/g, "\n").trim();
  if (!normalized) return ["roleImages.validation.dockerfileRequired"];
  const meaningful = normalized
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"));
  if (!meaningful.some((line) => /^FROM\s+/i.test(line)))
    return ["roleImages.validation.fromRequired"];
  return [];
}

export function defaultDockerfile(): string {
  return [
    "# Пользовательская часть образа. Runtime contract Kodex добавит платформа.",
    "FROM ubuntu:24.04",
    "",
    "RUN apt-get update \\",
    "    && apt-get install -y --no-install-recommends ca-certificates curl \\",
    "    && rm -rf /var/lib/apt/lists/*",
    "",
  ].join("\n");
}
