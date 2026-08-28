export function projectPath(projectRef: string): string {
  return `/projects/${encodeURIComponent(projectRef)}`;
}

export function projectRunPath(projectRef: string, runRef: string): string {
  return `${projectPath(projectRef)}/runs/${encodeURIComponent(runRef)}`;
}

export function runPath(runRef: string, projectRef?: string): string {
  return projectRef
    ? projectRunPath(projectRef, runRef)
    : `/runs/${encodeURIComponent(runRef)}`;
}
