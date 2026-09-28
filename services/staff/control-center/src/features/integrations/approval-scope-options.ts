// Перечень путей строится только из опубликованной входной схемы, а не из
// аргументов вызова модели. Авторитетную проверку повторяет control-plane.
export function approvalScopeOptions(rawSchema?: string): string[] {
  if (!rawSchema || rawSchema.length > 64 * 1024) return [];
  let schema: unknown;
  try {
    schema = JSON.parse(rawSchema);
  } catch {
    return [];
  }
  const result: string[] = [];
  const visit = (node: unknown, prefix: string, depth: number): void => {
    if (!node || typeof node !== "object" || Array.isArray(node)) return;
    const typed = node as Record<string, unknown>;
    if (typed.type !== "object" || depth >= 8) return;
    const properties = typed.properties;
    if (
      !properties ||
      typeof properties !== "object" ||
      Array.isArray(properties)
    )
      return;
    for (const [name, child] of Object.entries(properties)) {
      if (!name || !child || typeof child !== "object" || Array.isArray(child))
        continue;
      const path = `${prefix}/${name.replaceAll("~", "~0").replaceAll("/", "~1")}`;
      if (path.length > 256) continue;
      const childType = (child as Record<string, unknown>).type;
      if (
        childType === "string" ||
        childType === "integer" ||
        childType === "number" ||
        childType === "boolean" ||
        childType === "object" ||
        childType === "array"
      ) {
        result.push(path);
        if (result.length >= 256) return;
      }
      if (childType === "object") visit(child, path, depth + 1);
    }
  };
  visit(schema, "", 0);
  return result.sort();
}

export function validApprovalScopeSelection(
  selected: readonly string[],
  choices: readonly string[],
): boolean {
  if (selected.length === 0 || selected.length > 16) return false;
  const allowed = new Set(choices);
  return (
    new Set(selected).size === selected.length &&
    selected.every((path) => allowed.has(path))
  );
}
