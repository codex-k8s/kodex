export const populatedVariants = [
  "project-picker-async-search",
  "kanban-independent-scroll",
  "global-search-enter-clear",
  "project-eight-sections",
  ...[0, 1].map((slot) => `project-${String(slot)}-environment-inspector`),
  ...["prompt-template", "role-image", "integration-definition"].map(
    (kind) => `configuration-history-${kind}`,
  ),
  ...["ru", "en"].flatMap((locale) =>
    [1440, 768, 390].map(
      (width) => `assistant-history-search-${locale}-${String(width)}`,
    ),
  ),
  "fixture-assistant-history-pagination",
  "fixture-environment-inspector",
  "fixture-configuration-history",
  "fixture-configuration-source-keys",
  "fixture-role-image-project-source",
  "fixture-global-search-debounce",
  ...["project", "agent", "workflow", "run"].map(
    (kind) => `fixture-global-search-${kind}`,
  ),
  "fixture-vfs-active",
  "fixture-vfs-trash",
  "fixture-vfs-pagination",
  "fixture-kanban-pages",
] as const;
