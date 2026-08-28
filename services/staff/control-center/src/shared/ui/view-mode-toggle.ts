export type ViewMode = "list" | "grid";

export function viewModeFromNavigationKey(
  current: ViewMode,
  key: string,
): ViewMode | undefined {
  if (key === "Home") return "list";
  if (key === "End") return "grid";
  if (key === "ArrowLeft" || key === "ArrowUp")
    return current === "list" ? "grid" : "list";
  if (key === "ArrowRight" || key === "ArrowDown")
    return current === "grid" ? "list" : "grid";
  return undefined;
}
