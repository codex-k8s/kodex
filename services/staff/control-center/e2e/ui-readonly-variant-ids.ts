export const readonlyFormVariants = ["ru", "en"].flatMap((locale) =>
  [1440, 390].flatMap((width) => [
    `project-form-cancel-${locale}-${String(width)}`,
    `project-collection-expand-${locale}-${String(width)}`,
    `assistant-history-draft-${locale}-${String(width)}`,
    ...["prompt-template", "role-image", "integration-definition"].map(
      (kind) =>
        `configuration-create-editor-${kind}-${locale}-${String(width)}`,
    ),
  ]),
);
