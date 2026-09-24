<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  assistantPollDelay,
  assistantRoleImageBuildTarget,
} from "@/features/assistant/model";
import {
  commandRoleImage,
  loadRoleImageDetail,
} from "@/features/role-images/api";
import { latestBuild } from "@/features/role-images/model";
import type {
  AssistantPlan,
  RoleImageRecipeDetail,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const { t } = useI18n();
const target = computed(() =>
  assistantRoleImageBuildTarget(props.plan, props.operationRef),
);
const detail = ref<RoleImageRecipeDetail>();
const loading = ref(false);
const stopping = ref(false);
const problem = ref(false);
const build = computed(() => latestBuild(detail.value?.builds ?? []));
const cancellable = computed(
  () =>
    build.value &&
    !["COMPLETED", "CANCELLED", "DEAD_LETTER"].includes(build.value.stage),
);
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    detail.value = undefined;
    problem.value = false;
    if (!value) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    onCleanup(() => {
      controller.abort();
      if (timer) clearTimeout(timer);
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const next = await loadRoleImageDetail(
          value.projectRef,
          value.recipeRef,
          controller.signal,
        );
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (
          next.recipe.ref !== value.recipeRef ||
          next.recipe.projectRef !== value.projectRef ||
          next.builds.some((item) => item.recipeRef !== value.recipeRef)
        )
          throw new Error("Role image build scope mismatch");
        detail.value = next;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) {
          loading.value = false;
          attempts += 1;
          if (problem.value || cancellable.value)
            timer = setTimeout(
              () => void refresh?.(),
              assistantPollDelay(attempts),
            );
        }
      }
    };
    void refresh();
  },
  { immediate: true },
);

async function stopBuild(): Promise<void> {
  const current = detail.value?.recipe;
  const exact = target.value;
  if (
    !current ||
    !exact ||
    !cancellable.value ||
    !build.value ||
    !current.nextActions.includes("CANCEL_BUILD") ||
    stopping.value ||
    !window.confirm(t("assistant.roleImageBuild.stopConfirm"))
  )
    return;
  stopping.value = true;
  try {
    await commandRoleImage(
      exact.projectRef,
      current,
      "CANCEL_BUILD",
      build.value.ref,
    );
    await refresh?.();
  } catch {
    problem.value = true;
  } finally {
    stopping.value = false;
  }
}
</script>

<template>
  <section v-if="target" class="assistant-build-card" aria-live="polite">
    <header>
      <strong>{{ $t("assistant.roleImageBuild.title") }}</strong>
      <StatusBadge v-if="build" :state="build.stage" />
    </header>
    <p v-if="loading && !detail">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="assistant-build-card__problem" role="alert">
      {{ $t("assistant.roleImageBuild.loadFailed") }}
    </p>
    <template v-if="detail">
      <p>{{ detail.recipe.name }}</p>
      <template v-if="build">
        <label>
          {{
            $t("assistant.roleImageBuild.progress", {
              progress: build.progressPercent,
            })
          }}
          <progress :value="build.progressPercent" max="100" />
        </label>
        <p v-if="build.safeErrorCode" class="assistant-build-card__problem">
          {{ build.safeErrorCode }}
        </p>
        <p
          v-if="
            build.stage === 'COMPLETED' && !detail.recipe.promotedImageReady
          "
        >
          {{ $t("assistant.roleImageBuild.awaitingPromotion") }}
        </p>
        <p v-if="detail.recipe.promotedImageReady">
          {{ $t("assistant.roleImageBuild.ready") }}
        </p>
      </template>
      <p v-else>{{ $t("assistant.roleImageBuild.noBuild") }}</p>
    </template>
    <div class="assistant-build-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading || stopping"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        class="button"
        :to="{
          name: 'role-image',
          params: {
            projectRef: target.projectRef,
            recipeRef: target.recipeRef,
          },
        }"
        @click="emit('navigate')"
      >
        {{ $t("assistant.roleImageBuild.open") }}
      </RouterLink>
      <button
        v-if="
          cancellable && detail?.recipe.nextActions.includes('CANCEL_BUILD')
        "
        class="button button--danger"
        type="button"
        :disabled="stopping || loading"
        @click="stopBuild"
      >
        {{ $t("assistant.roleImageBuild.stop") }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.assistant-build-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-build-card header,
.assistant-build-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-build-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-build-card label {
  display: grid;
  gap: 4px;
}
.assistant-build-card progress {
  width: 100%;
}
.assistant-build-card__problem {
  color: var(--danger);
}
</style>
