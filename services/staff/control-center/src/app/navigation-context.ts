export type NavigationSection =
  | "home"
  | "projects"
  | "runs"
  | "decisions"
  | "integrations"
  | "administration"
  | "project"
  | "agents"
  | "workflows"
  | "project-runs"
  | "files"
  | "automations"
  | "runtime-environments"
  | "runtime-secrets"
  | "project-access";

const routeSections: Readonly<Record<string, NavigationSection>> = {
  home: "home",
  projects: "projects",
  runs: "runs",
  run: "runs",
  decisions: "decisions",
  integrations: "integrations",
  administration: "administration",
  access: "administration",
  audit: "administration",
  project: "project",
  agents: "agents",
  agent: "agents",
  workflows: "workflows",
  workflow: "workflows",
  "new-run": "project-runs",
  "project-runs": "project-runs",
  "project-run": "project-runs",
  files: "files",
  automations: "automations",
  "runtime-environments": "runtime-environments",
  "runtime-environment-new": "runtime-environments",
  "runtime-environment": "runtime-environments",
  "runtime-secrets": "runtime-secrets",
  "project-access": "project-access",
};

export function activeNavigationSection(
  routeName: unknown,
): NavigationSection | undefined {
  return typeof routeName === "string" ? routeSections[routeName] : undefined;
}

export function routeProjectRef(
  params: Record<string, unknown>,
): string | undefined {
  const value = params.projectRef;
  return typeof value === "string" && value.length > 0 ? value : undefined;
}
