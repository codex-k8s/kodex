export type RuntimeResourceScope =
  | { kind: "ORGANIZATION" }
  | { kind: "PROJECT"; projectRef: string };

export function runtimeResourceScopeKey(scope: RuntimeResourceScope): string {
  if (scope.kind === "ORGANIZATION") return "ORGANIZATION";
  if (!/^[A-Za-z0-9_-]{8,128}$/.test(scope.projectRef))
    throw new Error("Runtime resource project scope is invalid");
  return `PROJECT:${scope.projectRef}`;
}

export function assertRuntimeResourceScope(
  scope: RuntimeResourceScope,
  projectRef: string | undefined,
): void {
  runtimeResourceScopeKey(scope);
  if (
    scope.kind === "ORGANIZATION"
      ? Boolean(projectRef)
      : projectRef !== scope.projectRef
  )
    throw new Error("Runtime resource scope mismatch");
}
