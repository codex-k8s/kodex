import type {
  Agent,
  Project,
  ProviderAccount,
  RoleImageRecipe,
  Run,
  RuntimeEnvironmentSet,
  SystemAssistant,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

export const onboardingSteps = [
  "model",
  "project",
  "image",
  "environment",
  "team",
  "launch",
] as const;
export type OnboardingStep = (typeof onboardingSteps)[number];

export function onboardingStep(value: unknown): OnboardingStep {
  return onboardingSteps.find((step) => step === value) ?? "model";
}

export function onboardingNavigation(value: unknown): {
  step: OnboardingStep;
  projectRef: string;
} {
  const saved = value && typeof value === "object" ? value : {};
  const projectRef = "projectRef" in saved ? saved.projectRef : undefined;
  return {
    step: onboardingStep("step" in saved ? saved.step : undefined),
    projectRef:
      typeof projectRef === "string" &&
      /^prj_[A-Za-z0-9_-]{1,123}$/.test(projectRef)
        ? projectRef
        : "",
  };
}

export function onboardingProgress(input: {
  providerAccounts: readonly ProviderAccount[];
  assistant?: SystemAssistant;
  project?: Project;
  images: readonly RoleImageRecipe[];
  environments: readonly RuntimeEnvironmentSet[];
  agents: readonly Agent[];
  workflows: readonly Workflow[];
  runs: readonly Run[];
}) {
  const modelReady = input.providerAccounts.some(
    (account) =>
      account.enabled && account.ready && account.state === "AUTHORIZED",
  );
  const assistantReady =
    modelReady &&
    input.assistant?.runtimeState === "READY" &&
    input.assistant.nextActions.includes("CREATE_CONVERSATION");
  const projectRef =
    input.project?.lifecycle === "ACTIVE" ? input.project.ref : undefined;
  const imageReady = Boolean(
    projectRef &&
    input.images.some(
      (image) =>
        image.projectRef === projectRef &&
        image.state === "ACTIVE" &&
        image.promotedImageReady,
    ),
  );
  const environmentReady = Boolean(
    projectRef &&
    input.environments.some(
      (environment) =>
        environment.projectRef === projectRef &&
        environment.state === "ACTIVE" &&
        environment.ready,
    ),
  );
  const teamReady = Boolean(
    projectRef &&
    input.agents.some(
      (agent) =>
        agent.projectRef === projectRef &&
        !agent.system &&
        agent.enabled &&
        agent.runtimeReady &&
        ["READY", "RUNNING"].includes(agent.state),
    ),
  );
  const firstRunReady = Boolean(
    projectRef &&
    input.runs.some(
      (run) =>
        run.projectRef === projectRef &&
        run.state === "SUCCEEDED" &&
        ((run.target.type === "AGENT" &&
          input.agents.some(
            (agent) =>
              !agent.system &&
              agent.projectRef === projectRef &&
              agent.ref === run.target.ref,
          )) ||
          (run.target.type === "WORKFLOW" &&
            input.workflows.some(
              (workflow) =>
                workflow.projectRef === projectRef &&
                workflow.ref === run.target.ref,
            ))),
    ),
  );
  const complete = {
    model: modelReady,
    project: Boolean(projectRef),
    image: imageReady,
    environment: environmentReady,
    team: teamReady,
    launch: firstRunReady,
  } satisfies Record<OnboardingStep, boolean>;
  return {
    modelReady,
    assistantReady,
    complete,
    completedCount: Object.values(complete).filter(Boolean).length,
  };
}
