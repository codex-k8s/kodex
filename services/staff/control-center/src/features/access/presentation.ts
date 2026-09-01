type PermissionMessages = Record<
  string,
  { name?: unknown; description?: unknown }
>;

const accessScopeKinds = new Set([
  "ORGANIZATION",
  "PROJECT",
  "RESOURCE_KIND",
  "RESOURCE_INSTANCE",
]);

export function accessScopeKind(scope: unknown): string | undefined {
  if (!scope || typeof scope !== "object") return undefined;
  const kind = (scope as { kind?: unknown }).kind;
  return typeof kind === "string" && accessScopeKinds.has(kind)
    ? kind
    : undefined;
}

export function permissionMessage(
  messages: unknown,
  permissionKey: string,
  field: "name" | "description",
): string {
  if (!messages || typeof messages !== "object") return permissionKey;
  const value = (messages as PermissionMessages)[permissionKey]?.[field];
  return typeof value === "string" && value.trim() ? value : permissionKey;
}
