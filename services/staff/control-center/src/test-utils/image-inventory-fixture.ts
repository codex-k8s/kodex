import type {
  ImageToolInventory,
  ImageToolObservation,
  ImagePlatformToolInventory,
} from "../shared/api/generated/openapi/types.gen";

// Полный синтетический probe snapshot: только тестовые дайджесты и программы.
const names: ImageToolObservation["name"][] = [
  "bash",
  "curl",
  "git",
  "gh",
  "jq",
  "yq",
  "ripgrep",
  "make",
  "just",
  "go",
  "goimports",
  "gofumpt",
  "golangci-lint",
  "staticcheck",
  "goose",
  "sqlc",
  "buf",
  "protoc",
  "protoc-gen-go",
  "protoc-gen-go-grpc",
  "grpcurl",
  "mockgen",
  "oapi-codegen",
  "node",
  "npm",
  "pnpm",
  "yarn",
  "typescript",
  "eslint",
  "prettier",
  "vite",
  "vue-tsc",
  "vitest",
  "playwright",
  "chromium",
  "playwright-mcp",
  "wscat",
  "codex",
  "corepack",
  "python3",
  "pip",
  "kubectl",
  "kustomize",
  "helm",
  "buildctl",
  "docker",
  "shellcheck",
  "hadolint",
  "govulncheck",
  "gitleaks",
];

export function unavailableInventoryFixture(): ImageToolInventory {
  return {
    status: "UNAVAILABLE",
    sha256: "",
    imageDigest: "",
    provenanceSha256: "",
    platforms: [],
  };
}

export function verifiedInventoryFixture(
  manifestDigest = `sha256:${"a".repeat(64)}`,
  provenanceSha256 = "b".repeat(64),
  verified: readonly ImageToolObservation["name"][] = names,
): ImageToolInventory {
  const tools = names.map((name, index): ImageToolObservation => {
    const command =
      name === "ripgrep" ? "rg" : name === "typescript" ? "tsc" : name;
    const present = verified.includes(name);
    return {
      name,
      required: index < 38,
      status: present ? "VERIFIED" : "MISSING",
      path: present ? `/usr/local/bin/${command}` : "",
      version: present ? "1.2.3" : "",
      sha256: present ? "c".repeat(64) : "",
    };
  });
  if (tools.length !== 50)
    throw new Error("Invalid synthetic inventory cardinality");
  return {
    status: "VERIFIED",
    sha256: "d".repeat(64),
    imageDigest: `sha256:${manifestDigest.replace(/^sha256:/, "")}`,
    provenanceSha256,
    platforms: [
      {
        platform: "linux/amd64",
        platformDigest: `sha256:${"e".repeat(64)}`,
        manifestSha256: "f".repeat(64),
        tools: tools as ImagePlatformToolInventory["tools"],
      },
    ],
  };
}
