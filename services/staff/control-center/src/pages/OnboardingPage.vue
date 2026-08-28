<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { useContextualAssistant } from "@/features/assistant/contextual-assistant";

const platform = usePlatformStore();
const contextualAssistant = useContextualAssistant();
const router = useRouter();
const busy = ref(false);
const problem = ref<AppProblem>();
const assistantReady = computed(
  () => platform.bootstrap?.assistant.runtimeState === "READY",
);
const canFinish = computed(() =>
  platform.bootstrap?.nextActions.includes("COMPLETE_ONBOARDING"),
);

async function finish(): Promise<void> {
  if (!canFinish.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.finishOnboarding();
    await router.push("/projects");
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(async () => {
  await platform.loadBootstrap();
  if (platform.bootstrap?.onboardingComplete) await router.replace("/");
});
</script>

<template>
  <PageFrame
    :title="$t('onboarding.title')"
    :subtitle="$t('onboarding.subtitle')"
  >
    <AsyncState
      :loading="platform.loading.bootstrap"
      :problem="platform.problems.bootstrap"
      @retry="platform.loadBootstrap()"
    >
      <section class="onboarding-panel">
        <div class="assistant-ready">
          <div class="assistant-orb" aria-hidden="true">✦</div>
          <div>
            <h2>{{ $t("onboarding.ready") }}</h2>
            <p>{{ $t("onboarding.webOnly") }}</p>
          </div>
          <StatusBadge
            :state="
              platform.bootstrap?.assistant.runtimeState ?? 'PROVISIONING'
            "
          />
        </div>
        <ol class="onboarding-steps">
          <li><span>1</span>{{ $t("onboarding.stepProject") }}</li>
          <li><span>2</span>{{ $t("onboarding.stepAgent") }}</li>
          <li><span>3</span>{{ $t("onboarding.stepRun") }}</li>
        </ol>
        <ProblemNotice v-if="problem" :problem="problem" compact />
        <div class="onboarding-actions">
          <button
            class="button button--primary button--large"
            type="button"
            :disabled="!assistantReady"
            @click="contextualAssistant.show"
          >
            {{ $t("onboarding.startAssistant") }}
          </button>
          <button
            v-if="canFinish"
            class="button button--secondary button--large"
            type="button"
            :disabled="busy || !assistantReady"
            @click="finish"
          >
            {{ $t("onboarding.finish") }}
          </button>
        </div>
      </section>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.onboarding-panel {
  max-width: 850px;
  padding: 26px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
}
.assistant-ready {
  display: flex;
  align-items: center;
  gap: 16px;
  padding-bottom: 22px;
  border-bottom: 1px solid var(--border);
}
.assistant-ready > div:nth-child(2) {
  flex: 1;
}
.assistant-ready h2,
.assistant-ready p {
  margin-bottom: 3px;
}
.assistant-orb {
  display: grid;
  place-items: center;
  width: 52px;
  height: 52px;
  border-radius: 14px;
  color: var(--accent);
  background: var(--accent-soft);
  font-size: 1.5rem;
}
.onboarding-steps {
  display: grid;
  gap: 16px;
  padding: 24px 0;
  list-style: none;
}
.onboarding-steps li {
  display: flex;
  align-items: center;
  gap: 12px;
  font-weight: 500;
}
.onboarding-steps span {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  color: white;
  background: var(--accent);
}
.onboarding-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
</style>
