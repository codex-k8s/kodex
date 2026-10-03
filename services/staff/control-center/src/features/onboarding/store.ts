import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import { useProvidersStore } from "@/features/providers/store";
import { useRuntimeStore } from "@/features/runtime/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  onboardingProgress,
  onboardingNavigation,
  onboardingStep,
  type OnboardingStep,
} from "./model";

export const useOnboardingStore = defineStore("onboarding", () => {
  const platform = usePlatformStore();
  const providers = useProvidersStore();
  const runtime = useRuntimeStore();
  const step = ref<OnboardingStep>("model");
  const projectRef = ref("");
  const busy = ref(false);
  const problem = ref<AppProblem>();
  let restoringNavigation = false;
  const projects = computed(() =>
    platform.projectList.filter((project) => project.lifecycle === "ACTIVE"),
  );
  const project = computed(() =>
    projects.value.find((item) => item.ref === projectRef.value),
  );
  const hasProjectRole = computed(() =>
    Boolean(
      project.value &&
      Object.values(platform.agents).some(
        (agent) =>
          agent.projectRef === project.value?.ref &&
          !agent.system &&
          agent.state !== "ARCHIVED" &&
          agent.roleDefinitionRef,
      ),
    ),
  );
  const progress = computed(() =>
    onboardingProgress({
      providerAccounts: providers.snapshotAccounts,
      assistant: platform.assistant ?? platform.bootstrap?.assistant,
      project: project.value,
      images: Object.values(platform.roleImageRecipes),
      environments: Object.values(runtime.environments),
      agents: Object.values(platform.agents),
      workflows: Object.values(platform.workflows),
      runs: Object.values(platform.runs),
    }),
  );
  const active = computed(() =>
    Boolean(platform.bootstrap && !platform.bootstrap.onboardingComplete),
  );
  const returnTo = computed(() => ({
    name: "onboarding",
    query: {
      step: step.value,
      ...(projectRef.value ? { projectRef: projectRef.value } : {}),
    },
  }));
  const canFinish = computed(() =>
    Boolean(platform.bootstrap?.nextActions.includes("COMPLETE_ONBOARDING")),
  );
  watch(
    () => platform.bootstrap?.currentUser.ref,
    (next, previous) => {
      if (next !== previous) {
        restoringNavigation = true;
        step.value = "model";
        projectRef.value = "";
        problem.value = undefined;
        busy.value = false;
        if (next && typeof window !== "undefined") {
          try {
            const saved = onboardingNavigation(
              JSON.parse(
                window.sessionStorage.getItem(`kodex:onboarding:${next}`) ??
                  "null",
              ),
            );
            step.value = saved.step;
            projectRef.value = saved.projectRef;
          } catch {
            // Ограничения браузерного хранилища не блокируют первичную настройку.
          }
        }
        restoringNavigation = false;
      }
    },
    { immediate: true },
  );
  watch(
    () => [step.value, projectRef.value] as const,
    ([step, projectRef]) => {
      const owner = platform.bootstrap?.currentUser.ref;
      if (restoringNavigation || !owner || typeof window === "undefined")
        return;
      try {
        window.sessionStorage.setItem(
          `kodex:onboarding:${owner}`,
          JSON.stringify({ step, projectRef }),
        );
      } catch {
        // Состояние навигации остаётся доступным в текущем сеансе приложения.
      }
    },
    { flush: "sync" },
  );
  function selectStep(value: unknown): void {
    step.value = onboardingStep(value);
  }
  function selectProject(value: string): void {
    projectRef.value = /^prj_[A-Za-z0-9_-]{1,123}$/.test(value) ? value : "";
  }
  async function finish(): Promise<boolean> {
    if (!canFinish.value || busy.value) return false;
    busy.value = true;
    problem.value = undefined;
    try {
      await platform.finishOnboarding();
      return true;
    } catch (error) {
      problem.value = asProblem(error);
      return false;
    } finally {
      busy.value = false;
    }
  }
  return {
    step,
    projectRef,
    project,
    hasProjectRole,
    projects,
    progress,
    active,
    returnTo,
    canFinish,
    busy,
    problem,
    selectStep,
    selectProject,
    finish,
  };
});
