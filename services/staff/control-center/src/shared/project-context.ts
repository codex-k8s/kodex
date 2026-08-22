let projectRef: string | undefined;

export function selectProjectRef(value?: string): void {
  projectRef = value;
}

export function selectedProjectRef(): string | undefined {
  return projectRef;
}
