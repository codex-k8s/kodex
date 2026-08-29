export interface Breadcrumb {
  label: string;
  path?: string;
}

export interface BreadcrumbLabels {
  home: string;
  onboarding: string;
  projects: string;
  project: string;
  agents: string;
  agent: string;
  workflows: string;
  workflow: string;
  newRun: string;
  runs: string;
  run: string;
  files: string;
  automations: string;
  environments: string;
  environment: string;
  newEnvironment: string;
  secrets: string;
  integrations: string;
  decisions: string;
  administration: string;
  access: string;
  audit: string;
}

export interface BreadcrumbContext {
  routeName?: string;
  project?: { ref: string; name: string };
  agentName?: string;
  workflowName?: string;
  runName?: string;
  environmentName?: string;
}

function current(label: string): Breadcrumb {
  return { label };
}

function projectTrail(
  context: BreadcrumbContext,
  labels: BreadcrumbLabels,
): Breadcrumb[] {
  const result: Breadcrumb[] = [{ label: labels.projects, path: "/projects" }];
  if (context.project) {
    result.push({
      label: context.project.name,
      path: `/projects/${encodeURIComponent(context.project.ref)}`,
    });
  } else {
    result.push({ label: labels.project, path: "/projects" });
  }
  return result;
}

export function buildBreadcrumbs(
  context: BreadcrumbContext,
  labels: BreadcrumbLabels,
): Breadcrumb[] {
  const project = projectTrail(context, labels);
  switch (context.routeName) {
    case "home":
      return [current(labels.home)];
    case "onboarding":
      return [current(labels.onboarding)];
    case "projects":
      return [current(labels.projects)];
    case "project":
      return project.map((item, index) =>
        index === project.length - 1 ? current(item.label) : item,
      );
    case "agents":
      return [...project, current(labels.agents)];
    case "agent":
      return [
        ...project,
        {
          label: labels.agents,
          path: context.project
            ? `/projects/${encodeURIComponent(context.project.ref)}/agents`
            : "/projects",
        },
        current(context.agentName ?? labels.agent),
      ];
    case "workflows":
      return [...project, current(labels.workflows)];
    case "workflow":
      return [
        ...project,
        {
          label: labels.workflows,
          path: context.project
            ? `/projects/${encodeURIComponent(context.project.ref)}/workflows`
            : "/projects",
        },
        current(context.workflowName ?? labels.workflow),
      ];
    case "new-run":
      return [
        ...project,
        {
          label: labels.runs,
          path: context.project
            ? `/projects/${encodeURIComponent(context.project.ref)}/runs`
            : "/runs",
        },
        current(labels.newRun),
      ];
    case "project-runs":
      return [...project, current(labels.runs)];
    case "runs":
      return [current(labels.runs)];
    case "run":
      return [
        { label: labels.runs, path: "/runs" },
        current(context.runName ?? labels.run),
      ];
    case "project-run":
      return [
        ...project,
        {
          label: labels.runs,
          path: context.project
            ? `/projects/${encodeURIComponent(context.project.ref)}/runs`
            : "/runs",
        },
        current(context.runName ?? labels.run),
      ];
    case "files":
      return [...project, current(labels.files)];
    case "automations":
      return [...project, current(labels.automations)];
    case "runtime-environments":
      return [...project, current(labels.environments)];
    case "runtime-environment-new":
      return [
        ...project,
        {
          label: labels.environments,
          path: context.project
            ? `/projects/${encodeURIComponent(context.project.ref)}/environments`
            : "/projects",
        },
        current(labels.newEnvironment),
      ];
    case "runtime-environment":
      return [
        ...project,
        {
          label: labels.environments,
          path: context.project
            ? `/projects/${encodeURIComponent(context.project.ref)}/environments`
            : "/projects",
        },
        current(context.environmentName ?? labels.environment),
      ];
    case "runtime-secrets":
      return [...project, current(labels.secrets)];
    case "integrations":
      return [current(labels.integrations)];
    case "decisions":
      return [current(labels.decisions)];
    case "access":
      return [
        { label: labels.administration, path: "/administration" },
        current(labels.access),
      ];
    case "project-access":
      return [...project, current(labels.access)];
    case "administration":
      return [current(labels.administration)];
    case "audit":
      return [
        { label: labels.administration, path: "/administration" },
        current(labels.audit),
      ];
    default:
      return [current(labels.home)];
  }
}
