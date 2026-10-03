<script setup lang="ts">
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  CalendarClock,
  Check,
  Container,
  Cpu,
  FolderKanban,
  Layers3,
  Play,
  Sparkles,
  UsersRound,
} from "@lucide/vue";
import { computed, onMounted, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { usePlatformStore } from "@/features/platform/store";
import { useProvidersStore } from "@/features/providers/store";
import ProjectPicker from "@/features/projects/ProjectPicker.vue";
import {
  requestAssistantSetup,
  requestAssistantSettings,
  type AssistantSetupRequest,
} from "@/features/assistant/events";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { requestConfirmation } from "@/shared/ui/confirmation";
import { onboardingSteps, type OnboardingStep } from "../model";
import { useOnboardingStore } from "../store";

const guide = useOnboardingStore();
const platform = usePlatformStore();
const providers = useProvidersStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const icons = {
  model: Cpu,
  assistant: Bot,
  project: FolderKanban,
  image: Container,
  environment: Layers3,
  team: UsersRound,
  launch: Play,
};
const index = computed(() => onboardingSteps.indexOf(guide.step));
const projectPath = computed(() =>
  guide.project
    ? `/projects/${encodeURIComponent(guide.project.ref)}`
    : "/projects",
);
const providerLoading = computed(
  () => !providers.snapshotReady && !providers.problem,
);
const assistant = computed(
  () => platform.assistant ?? platform.bootstrap?.assistant,
);
const assistantLabel = computed(() =>
  providerLoading.value
    ? t("common.loading")
    : providers.problem
      ? t("onboarding.providerUnknown")
      : !guide.progress.modelReady
        ? t("onboarding.modelRequired")
        : guide.progress.assistantReady
          ? t("onboarding.assistantReady")
          : assistant.value?.runtimeState === "FAILED"
            ? t("onboarding.assistantFailed")
            : t("onboarding.assistantPreparing"),
);

function navigateStep(step: OnboardingStep): void {
  guide.selectStep(step);
  void router.replace(guide.returnTo);
}
function selectProject(projectRef: string): void {
  guide.selectProject(projectRef);
  void router.replace(guide.returnTo);
}
async function startAssistant(): Promise<void> {
  if (
    !guide.progress.assistantReady ||
    guide.step === "model" ||
    guide.step === "assistant"
  )
    return;
  const steps: Record<
    Exclude<OnboardingStep, "model" | "assistant">,
    AssistantSetupRequest["step"]
  > = {
    project: "project",
    image: "image",
    environment: "environment",
    team: "team",
    launch: "launch",
  };
  const step = steps[guide.step];
  if (guide.step !== "project") {
    if (!guide.project) return;
    await router.push(projectPath.value);
    await nextTick();
  }
  requestAssistantSetup(step);
}
async function finish(): Promise<void> {
  if (!guide.active) {
    await router.push("/");
    return;
  }
  if (await guide.finish()) await router.push("/");
}
async function finishLater(): Promise<void> {
  if (!guide.canFinish || guide.busy) return;
  if (
    await requestConfirmation({
      title: t("onboarding.finishLater"),
      message: t("onboarding.finishLaterConfirm"),
      confirmLabel: t("onboarding.finishLater"),
    })
  )
    await finish();
}
watch(
  () => [
    route.query.step,
    route.query.projectRef,
    platform.bootstrap?.currentUser.ref,
  ],
  () => {
    if (route.name !== "onboarding") return;
    guide.selectStep(route.query.step);
    if (typeof route.query.projectRef === "string")
      guide.selectProject(route.query.projectRef);
  },
  { immediate: true },
);
watch(
  () => guide.projects,
  (projects) => {
    if (!guide.projectRef && projects.length === 1 && projects[0]) {
      guide.selectProject(projects[0].ref);
      void router.replace(guide.returnTo);
    }
  },
  { immediate: true },
);
onMounted(() => {
  if (!platform.bootstrap && !platform.loading.bootstrap)
    void platform.loadBootstrap();
  if (!providers.snapshotReady) void providers.load();
});
</script>

<template>
  <section class="setup" :aria-label="$t('onboarding.title')">
    <aside class="setup-sidebar">
      <div class="setup-progress">
        <strong>{{ $t("onboarding.yourSetup") }}</strong
        ><span>{{
          $t("onboarding.progress", {
            count: guide.progress.completedCount,
            total: onboardingSteps.length,
          })
        }}</span>
      </div>
      <div class="setup-progress-track" aria-hidden="true">
        <span
          :style="{
            width: `${(guide.progress.completedCount / onboardingSteps.length) * 100}%`,
          }"
        />
      </div>
      <nav :aria-label="$t('onboarding.stepsLabel')">
        <ol>
          <li v-for="(step, stepIndex) in onboardingSteps" :key="step">
            <button
              type="button"
              :class="{
                selected: guide.step === step,
                complete: guide.progress.complete[step],
              }"
              :aria-current="guide.step === step ? 'step' : undefined"
              @click="navigateStep(step)"
            >
              <span class="step-marker"
                ><Check
                  v-if="guide.progress.complete[step]"
                  :size="17"
                  aria-hidden="true"
                /><span v-else>{{ stepIndex + 1 }}</span></span
              >
              <span class="step-copy"
                ><strong>{{ $t(`onboarding.steps.${step}.title`) }}</strong
                ><small>{{ $t(`onboarding.steps.${step}.short`) }}</small></span
              >
            </button>
          </li>
        </ol>
      </nav>
      <p class="setup-saving">{{ $t("onboarding.saveHelp") }}</p>
    </aside>
    <div class="setup-main">
      <div class="setup-main-header">
        <span class="step-icon"
          ><component :is="icons[guide.step]" :size="24" aria-hidden="true"
        /></span>
        <div>
          <p class="eyebrow">
            {{
              $t("onboarding.currentStep", {
                step: index + 1,
                total: onboardingSteps.length,
              })
            }}
          </p>
          <h2>{{ $t(`onboarding.steps.${guide.step}.title`) }}</h2>
        </div>
        <span v-if="guide.progress.complete[guide.step]" class="setup-done"
          ><Check :size="15" />{{ $t("onboarding.done") }}</span
        >
      </div>
      <p class="setup-description">
        {{ $t(`onboarding.steps.${guide.step}.description`) }}
      </p>
      <div
        class="setup-assistant"
        :class="{ 'setup-assistant--ready': guide.progress.assistantReady }"
      >
        <Bot :size="18" aria-hidden="true" /><span>{{ assistantLabel }}</span
        ><RouterLink
          v-if="guide.progress.modelReady && !guide.progress.assistantReady"
          to="/administration"
          >{{ $t("onboarding.assistantSettings") }}</RouterLink
        >
      </div>
      <ProblemNotice
        v-if="guide.problem || providers.problem || platform.problems.bootstrap"
        :problem="
          guide.problem ?? providers.problem ?? platform.problems.bootstrap!
        "
        compact
      />

      <div class="setup-body">
        <template v-if="guide.step === 'model'">
          <div class="setup-detail">
            <strong>{{
              $t(
                guide.progress.modelReady
                  ? "onboarding.connectedProviderTitle"
                  : "onboarding.connectProviderTitle",
              )
            }}</strong>
            <p>
              {{
                $t(
                  guide.progress.modelReady
                    ? "onboarding.connectedProviderHelp"
                    : "onboarding.connectProviderHelp",
                )
              }}
            </p>
            <RouterLink
              class="button button--primary"
              :to="{
                name: 'provider-accounts',
                query: { returnTo: router.resolve(guide.returnTo).href },
              }"
              >{{
                $t(
                  guide.progress.modelReady
                    ? "onboarding.manageModel"
                    : "onboarding.connectModel",
                )
              }}<ArrowRight :size="16" aria-hidden="true"
            /></RouterLink>
          </div>
          <div class="setup-note">
            <Cpu :size="18" aria-hidden="true" />
            <div>
              <strong>gpt-6.1-sol · medium</strong>
              <p>{{ $t("onboarding.defaultModelHelp") }}</p>
            </div>
          </div>
          <p class="setup-hint">
            {{
              $t(
                guide.progress.modelReady
                  ? "onboarding.continueAfterModel"
                  : "onboarding.manualWithoutModel",
              )
            }}
          </p>
        </template>
        <template v-else-if="guide.step === 'assistant'">
          <div class="setup-detail">
            <strong>{{ $t("onboarding.assistantSettings") }}</strong>
            <p>{{ $t("onboarding.assistantConfigurationHelp") }}</p>
            <button
              class="button button--primary"
              type="button"
              :disabled="!assistant?.nextActions.includes('EDIT')"
              @click="requestAssistantSettings"
            >
              <Bot :size="16" aria-hidden="true" />{{
                $t("onboarding.configureAssistant")
              }}
            </button>
          </div>
          <div class="setup-note">
            <Check :size="19" aria-hidden="true" />
            <div>
              <strong>{{ $t("onboarding.assistantDefaultsTitle") }}</strong>
              <p>{{ $t("onboarding.assistantDefaultsHelp") }}</p>
            </div>
          </div>
        </template>
        <template v-else-if="guide.step === 'project'">
          <label v-if="guide.projects.length" class="setup-project"
            ><span>{{ $t("onboarding.selectProject") }}</span
            ><ProjectPicker
              :project="guide.project"
              :placeholder="$t('onboarding.chooseProject')"
              @select="selectProject"
          /></label>
          <div class="setup-detail">
            <strong>{{ $t("onboarding.projectTitle") }}</strong>
            <p>{{ $t("onboarding.projectHelp") }}</p>
            <RouterLink
              class="button button--primary"
              to="/projects?create=1"
              >{{ $t("onboarding.createProject") }}</RouterLink
            ><RouterLink
              v-if="guide.project"
              class="button"
              :to="projectPath"
              >{{ $t("onboarding.openProject") }}</RouterLink
            >
          </div>
        </template>
        <div v-else-if="!guide.project" class="setup-detail">
          <strong>{{ $t("onboarding.projectRequired") }}</strong>
          <p>{{ $t("onboarding.projectRequiredHelp") }}</p>
          <button
            type="button"
            class="button button--primary"
            @click="navigateStep('project')"
          >
            {{ $t("onboarding.goToProject") }}
          </button>
        </div>
        <template v-else-if="guide.step === 'image'">
          <div class="setup-detail">
            <strong>{{ $t("onboarding.baseImage") }}</strong>
            <p>{{ $t("onboarding.baseImageHelp") }}</p>
            <button
              type="button"
              class="button button--primary"
              @click="navigateStep('environment')"
            >
              {{ $t("onboarding.useBaseImage") }}
            </button>
          </div>
          <div class="setup-detail setup-detail--secondary">
            <strong>{{ $t("onboarding.customImage") }}</strong>
            <p>{{ $t("onboarding.customImageHelp") }}</p>
            <RouterLink
              v-if="guide.hasProjectRole"
              class="button"
              :to="`${projectPath}/role-images/new`"
              >{{ $t("onboarding.configureImage") }}</RouterLink
            ><template v-else>
              <p>{{ $t("onboarding.imageNeedsRole") }}</p>
              <RouterLink
                class="button"
                :to="`${projectPath}/agents?create=1`"
                >{{ $t("onboarding.createEmployeeForImage") }}</RouterLink
              > </template
            ><RouterLink
              class="button button--text"
              :to="`${projectPath}/role-images`"
              >{{ $t("onboarding.existingImages") }}</RouterLink
            >
          </div>
        </template>
        <template v-else-if="guide.step === 'environment'">
          <div class="setup-detail">
            <strong>{{ $t("onboarding.environmentTitle") }}</strong>
            <p>{{ $t("onboarding.environmentHelp") }}</p>
            <template v-if="!guide.progress.complete.environment">
              <p>{{ $t("onboarding.defaultEnvironmentHelp") }}</p>
              <button
                type="button"
                class="button button--primary"
                @click="navigateStep('team')"
              >
                {{ $t("onboarding.useDefaultEnvironment") }}
              </button>
            </template>
            <RouterLink
              v-if="guide.progress.complete.environment"
              class="button button--primary"
              :to="`${projectPath}/environments`"
              >{{ $t("onboarding.configureEnvironment") }}</RouterLink
            ><RouterLink
              v-if="guide.progress.complete.image"
              class="button"
              :to="`${projectPath}/environments/new`"
              >{{ $t("onboarding.newEnvironment") }}</RouterLink
            >
          </div>
          <div class="setup-detail setup-detail--secondary">
            <strong
              >{{ $t("onboarding.secretsTitle")
              }}<span class="setup-optional">{{
                $t("onboarding.optional")
              }}</span></strong
            >
            <p>{{ $t("onboarding.secretsHelp") }}</p>
            <RouterLink class="button" :to="`${projectPath}/secrets`">{{
              $t("onboarding.configureSecrets")
            }}</RouterLink>
          </div>
        </template>
        <template v-else-if="guide.step === 'team'">
          <div class="setup-detail">
            <strong>{{ $t("onboarding.employeeTitle") }}</strong>
            <p>{{ $t("onboarding.employeeHelp") }}</p>
            <RouterLink
              class="button button--primary"
              :to="`${projectPath}/agents?create=1`"
              >{{ $t("onboarding.createEmployee") }}</RouterLink
            ><RouterLink
              class="button button--text"
              :to="`${projectPath}/agents`"
              >{{ $t("onboarding.existingEmployees") }}</RouterLink
            >
          </div>
          <div class="setup-detail setup-detail--secondary">
            <strong
              >{{ $t("onboarding.processTitle")
              }}<span class="setup-optional">{{
                $t("onboarding.optional")
              }}</span></strong
            >
            <p>{{ $t("onboarding.processHelp") }}</p>
            <RouterLink
              class="button"
              :to="`${projectPath}/workflows?create=1`"
              >{{ $t("onboarding.createProcess") }}</RouterLink
            >
          </div>
        </template>
        <template v-else-if="guide.step === 'launch'">
          <div class="setup-detail">
            <strong>{{ $t("onboarding.firstRunTitle") }}</strong>
            <p>{{ $t("onboarding.firstRunHelp") }}</p>
            <template v-if="!guide.progress.complete.team">
              <p>{{ $t("onboarding.firstRunNeedsEmployee") }}</p>
              <button
                type="button"
                class="button button--primary"
                @click="navigateStep('team')"
              >
                {{ $t("onboarding.goToEmployees") }}
              </button>
            </template>
            <RouterLink
              v-if="guide.progress.complete.team"
              class="button button--primary"
              :to="`${projectPath}/runs/new`"
              ><Play :size="16" aria-hidden="true" />{{
                $t("onboarding.firstRun")
              }}</RouterLink
            ><RouterLink
              class="button button--text"
              :to="`${projectPath}/runs`"
              >{{ $t("onboarding.results") }}</RouterLink
            >
          </div>
          <div class="setup-detail setup-detail--secondary">
            <strong
              >{{ $t("onboarding.automationTitle")
              }}<span class="setup-optional">{{
                $t("onboarding.optional")
              }}</span></strong
            >
            <p>{{ $t("onboarding.automationHelp") }}</p>
            <RouterLink class="button" :to="`${projectPath}/automations`"
              ><CalendarClock :size="16" aria-hidden="true" />{{
                $t("onboarding.configureAutomations")
              }}</RouterLink
            >
          </div>
        </template>
        <div
          v-if="
            guide.step !== 'model' &&
            guide.step !== 'assistant' &&
            (guide.step === 'project' || guide.project)
          "
          class="setup-with-assistant"
        >
          <div>
            <Sparkles :size="18" aria-hidden="true" /><span>{{
              $t("onboarding.assistantHelp")
            }}</span>
          </div>
          <button
            class="button"
            type="button"
            :disabled="!guide.progress.assistantReady"
            @click="startAssistant"
          >
            {{ $t("onboarding.startAssistant") }}</button
          ><small v-if="!guide.progress.assistantReady">{{
            $t("onboarding.assistantNeedsModel")
          }}</small>
        </div>
      </div>
      <footer class="setup-footer">
        <button
          class="button"
          type="button"
          :disabled="index === 0"
          @click="navigateStep(onboardingSteps[index - 1]!)"
        >
          <ArrowLeft :size="16" aria-hidden="true" />{{
            $t("onboarding.previous")
          }}</button
        ><button
          v-if="guide.active && guide.canFinish"
          class="button"
          type="button"
          :disabled="guide.busy"
          @click="finishLater"
        >
          {{ $t("onboarding.finishLater") }}</button
        ><span v-if="guide.project" class="setup-project-name">{{
          guide.project.name
        }}</span
        ><button
          v-if="index < onboardingSteps.length - 1"
          class="button button--primary"
          type="button"
          :disabled="
            !['model', 'assistant'].includes(guide.step) && !guide.project
          "
          @click="navigateStep(onboardingSteps[index + 1]!)"
        >
          {{ $t("onboarding.next")
          }}<ArrowRight :size="16" aria-hidden="true" /></button
        ><button
          v-else
          class="button button--primary"
          type="button"
          :disabled="(guide.active && !guide.canFinish) || guide.busy"
          @click="finish"
        >
          {{ $t("onboarding.finish") }}
        </button>
      </footer>
      <p v-if="guide.active" class="setup-finish-help">
        {{ $t("onboarding.finishHelp") }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.setup {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  max-width: 1260px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  overflow: hidden;
}
.setup-sidebar {
  background: var(--panel);
  padding: 26px 18px;
  border-right: 1px solid var(--border);
}
.setup-progress {
  display: grid;
  gap: 7px;
  padding: 0 12px;
}
.setup-progress span,
.setup-saving {
  color: var(--text-secondary);
  font-size: 12px;
}
.setup-progress-track {
  margin: 16px 12px 24px;
  height: 4px;
  border-radius: 4px;
  background: var(--border);
  overflow: hidden;
}
.setup-progress-track span {
  display: block;
  height: 100%;
  background: var(--accent);
  transition: width 0.2s;
}
.setup-sidebar ol {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 8px;
}
.setup-sidebar button {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 64px;
  padding: 12px;
  text-align: left;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--text);
  cursor: pointer;
}
.setup-sidebar button:hover {
  background: var(--surface);
}
.setup-sidebar button.selected {
  border-color: var(--border);
  background: var(--accent-soft);
}
.step-marker {
  flex: 0 0 30px;
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 50%;
  font-weight: 600;
  color: var(--text-secondary);
  background: var(--surface);
}
.selected .step-marker {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.complete .step-marker {
  color: var(--success);
  background: var(--surface);
}
.step-copy {
  display: grid;
  gap: 5px;
  min-width: 0;
}
.step-copy strong {
  font-size: 13px;
  line-height: 1.4;
}
.step-copy small {
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.4;
}
.setup-saving {
  padding: 0 12px;
  margin-top: 25px;
  line-height: 1.6;
}
.setup-main {
  min-width: 0;
  padding: 30px;
  display: flex;
  flex-direction: column;
}
.setup-main-header {
  display: flex;
  align-items: center;
  gap: 14px;
}
.setup-main-header h2 {
  margin: 3px 0 0;
  font-size: 22px;
}
.setup-main-header .eyebrow {
  margin: 0;
}
.step-icon {
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  flex-shrink: 0;
  border-radius: 12px;
  background: var(--accent-soft);
  color: var(--accent);
}
.setup-done {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-left: auto;
  color: var(--success);
  font-size: 12px;
}
.setup-description {
  margin: 20px 0 16px;
  line-height: 1.65;
  max-width: 700px;
  color: var(--text-secondary);
}
.setup-assistant {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 10px 12px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}
.setup-assistant--ready {
  color: var(--success);
}
.setup-assistant a {
  margin-left: auto;
}
.setup-body {
  display: grid;
  gap: 16px;
  margin: 22px 0 30px;
}
.setup-detail {
  padding: 20px;
  border: 1px solid var(--border);
  border-radius: 10px;
}
.setup-detail--secondary {
  background: var(--panel);
}
.setup-detail strong {
  display: block;
  font-size: 14px;
}
.setup-detail p {
  margin: 8px 0 16px;
  line-height: 1.6;
  color: var(--text-secondary);
  max-width: 660px;
}
.setup-detail .button + .button {
  margin-left: 8px;
}
.setup-note {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px 20px;
  background: var(--accent-soft);
  border-radius: 8px;
}
.setup-note svg {
  color: var(--accent);
  flex-shrink: 0;
}
.setup-note p {
  margin: 6px 0 0;
  color: var(--text-secondary);
  line-height: 1.6;
  font-size: 12px;
}
.setup-hint,
.setup-finish-help {
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.6;
}
.setup-project {
  display: grid;
  gap: 8px;
  font-weight: 600;
}
.setup-project select {
  width: 100%;
  max-width: 480px;
}
.setup-optional {
  font-weight: 400;
  font-size: 11px;
  color: var(--text-secondary);
  margin-left: 10px;
}
.setup-with-assistant {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  padding-top: 6px;
}
.setup-with-assistant > div {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 200px;
}
.setup-with-assistant svg {
  color: var(--accent);
  flex-shrink: 0;
}
.setup-with-assistant small {
  flex: 0 0 100%;
  color: var(--text-secondary);
}
.setup-footer {
  display: flex;
  align-items: center;
  gap: 16px;
  padding-top: 20px;
  margin-top: auto;
  border-top: 1px solid var(--border);
}
.setup-footer > :last-child {
  margin-left: auto;
}
.setup-project-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
  font-size: 12px;
}
.setup-finish-help {
  margin-top: 12px;
}
@media (max-width: 1000px) {
  .setup {
    grid-template-columns: 250px minmax(0, 1fr);
  }
  .setup-main {
    padding: 22px;
  }
}
@media (max-width: 700px) {
  .setup {
    grid-template-columns: 1fr;
  }
  .setup-sidebar {
    border-right: none;
    border-bottom: 1px solid var(--border);
  }
}
</style>
