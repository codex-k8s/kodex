// Эти проверки описывают только форму public contract; полномочия и native receipts проверяет сервер.
export function nativeWorkflowEvidenceProfile(profile: unknown): boolean {
  return profile === "GITHUB_REVIEW_QA_V1" || profile === "GITHUB_PDLC_V1";
}

export function validPDLCPullTarget(
  repository: unknown,
  number: unknown,
): boolean {
  return (
    typeof repository === "string" &&
    repository.length <= 200 &&
    /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/u.test(repository) &&
    typeof number === "string" &&
    /^[1-9][0-9]{0,9}$/u.test(number) &&
    Number(number) <= 2147483647
  );
}

export function validPDLCFileTarget(
  repository: unknown,
  branch: unknown,
  path: unknown,
  number: unknown,
): boolean {
  return (
    validPDLCPullTarget(repository, number) &&
    typeof branch === "string" &&
    /^[A-Za-z0-9][A-Za-z0-9_./-]{0,254}$/u.test(branch) &&
    !branch.startsWith("refs/") &&
    !branch.includes("..") &&
    !branch.endsWith(".") &&
    branch
      .split("/")
      .every(
        (part) =>
          part !== "" && !part.startsWith(".") && !part.endsWith(".lock"),
      ) &&
    typeof path === "string" &&
    path.length > 0 &&
    new TextEncoder().encode(path).length <= 1024 &&
    !["\\", "\0", "\r", "\n"].some((character) => path.includes(character)) &&
    !/[\uD800-\uDFFF]/u.test(path) &&
    path
      .split("/")
      .every((part) => part !== "" && part !== "." && part !== "..")
  );
}
