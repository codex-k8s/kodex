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
  promoteRoleImageArtifact,
} from "@/features/role-images/api";
import { canPromoteRoleImage, latestBuild } from "@/features/role-images/model";
import type {
  AssistantPlan,
  RoleImagePromotionReceipt,
  RoleImageRecipeDetail,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: []; debug: [prompt: string] }>();
const { t } = useI18n();
const target = computed(() =>
  assistantRoleImageBuildTarget(props.plan, props.operationRef),
);
const detail = ref<RoleImageRecipeDetail>();
const loading = ref(false);
const stopping = ref(false);
const promoting = ref(false);
const problem = ref(false);
const admissionTimedOut = ref(false);
const promotionTimedOut = ref(false);
const promotionProblem = ref(false);
const promotionReceipt = ref<RoleImagePromotionReceipt>();
const attemptedArtifactRef = ref<string>();
const build = computed(() => latestBuild(detail.value?.builds ?? []));
const candidate = computed(() => {
  const artifact = detail.value?.promotionCandidate;
  return build.value?.stage === "COMPLETED" &&
    artifact?.buildRef === build.value.ref &&
    artifact.recipeGeneration === build.value.recipeGeneration
    ? artifact
    : undefined;
});
const currentBuildPromoted = computed(
  () =>
    build.value?.stage === "COMPLETED" &&
    detail.value?.recipe.promotedImageReady === true &&
    detail.value.activeArtifact?.buildRef === build.value.ref &&
    detail.value.activeArtifact.recipeGeneration ===
      build.value.recipeGeneration &&
    (!detail.value.promotionCandidate ||
      detail.value.promotionCandidate.ref === detail.value.activeArtifact.ref),
);
const promotionFailed = computed(
  () =>
    (candidate.value?.promotionRequested === true &&
      candidate.value.promotionState === "REJECTED") ||
    promotionReceipt.value?.state === "FAILED",
);
const promotionPending = computed(
  () =>
    !currentBuildPromoted.value &&
    !promotionFailed.value &&
    !promotionTimedOut.value &&
    ((candidate.value?.promotionRequested === true &&
      ["PENDING", "CLAIMED", "AUTHORIZED"].includes(
        candidate.value.promotionState,
      )) ||
      ["QUEUED", "PROMOTING"].includes(promotionReceipt.value?.state ?? "")),
);
const promotionState = computed(() =>
  currentBuildPromoted.value
    ? "PROMOTED"
    : promotionFailed.value
      ? "FAILED"
      : candidate.value?.promotionRequested
        ? candidate.value.promotionState === "PENDING"
          ? "QUEUED"
          : "PROMOTING"
        : (promotionReceipt.value?.state ?? "PENDING"),
);
const awaitingAdmission = computed(
  () =>
    build.value?.stage === "COMPLETED" &&
    !candidate.value &&
    !currentBuildPromoted.value &&
    !admissionTimedOut.value,
);
const cancellable = computed(
  () =>
    build.value &&
    !["COMPLETED", "CANCELLED", "DEAD_LETTER"].includes(build.value.stage),
);
const debuggableFailure = computed(
  () =>
    Boolean(build.value) &&
    ["FAILED", "EXPIRED", "DEAD_LETTER"].includes(build.value?.stage ?? ""),
);
let refresh: (() => Promise<void>) | undefined;

function requestDebug(): void {
  const exact = target.value;
  const current = build.value;
  if (!exact || !current || !debuggableFailure.value) return;
  emit(
    "debug",
    t("assistant.roleImageBuild.debugPrompt", {
      recipeRef: exact.recipeRef,
      buildRef: current.ref,
      attempt: current.attempt,
      stage: current.stage,
      safeErrorCode: current.safeErrorCode || "NONE",
      diagnosticCode: current.diagnosticCode || "NONE",
      diagnosticSummary: current.diagnosticSummary || "NONE",
    }),
  );
}

watch(
  target,
  (value, _previous, onCleanup) => {
    detail.value = undefined;
    problem.value = false;
    admissionTimedOut.value = false;
    promotionTimedOut.value = false;
    promotionProblem.value = false;
    promotionReceipt.value = undefined;
    attemptedArtifactRef.value = undefined;
    if (!value) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    let admissionPolls = 0;
    let promotionPolls = 0;
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
          if (awaitingAdmission.value) {
            admissionPolls += 1;
            if (admissionPolls >= 120) admissionTimedOut.value = true;
          } else {
            admissionPolls = 0;
          }
          if (promotionPending.value) {
            promotionPolls += 1;
            if (promotionPolls >= 120) promotionTimedOut.value = true;
          } else {
            promotionPolls = 0;
          }
          if (
            problem.value ||
            cancellable.value ||
            awaitingAdmission.value ||
            promotionPending.value
          )
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

async function promoteCandidate(): Promise<void> {
  const exact = target.value;
  const recipe = detail.value?.recipe;
  const artifact = candidate.value;
  if (
    !exact ||
    !recipe ||
    !artifact ||
    !canPromoteRoleImage(recipe, artifact) ||
    promoting.value ||
    loading.value ||
    attemptedArtifactRef.value === artifact.ref ||
    !window.confirm(t("assistant.roleImageBuild.promoteConfirm"))
  )
    return;
  // После неопределённого ответа нельзя повторять state-changing command.
  attemptedArtifactRef.value = artifact.ref;
  promoting.value = true;
  promotionProblem.value = false;
  try {
    const receipt = await promoteRoleImageArtifact(
      exact.projectRef,
      recipe,
      artifact.ref,
      artifact.provenanceSha256,
    );
    if (target.value !== exact) return;
    if (
      receipt.recipeRef !== recipe.ref ||
      receipt.imageArtifactRef !== artifact.ref ||
      receipt.provenanceSha256 !== artifact.provenanceSha256
    )
      throw new Error("Role image promotion receipt mismatch");
    promotionReceipt.value = receipt;
    await refresh?.();
  } catch {
    if (target.value === exact) promotionProblem.value = true;
  } finally {
    promoting.value = false;
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
        <dl v-if="debuggableFailure" class="assistant-build-card__diagnostics">
          <div>
            <dt>{{ $t("assistant.roleImageBuild.buildRef") }}</dt>
            <dd>{{ build.ref }}</dd>
          </div>
          <div>
            <dt>{{ $t("assistant.roleImageBuild.attempt") }}</dt>
            <dd>{{ build.attempt }}</dd>
          </div>
          <div v-if="build.diagnosticCode">
            <dt>{{ $t("assistant.roleImageBuild.diagnosticCode") }}</dt>
            <dd>{{ build.diagnosticCode }}</dd>
          </div>
          <div v-if="build.diagnosticSummary">
            <dt>{{ $t("assistant.roleImageBuild.diagnosticSummary") }}</dt>
            <dd>{{ build.diagnosticSummary }}</dd>
          </div>
        </dl>
        <p
          v-if="
            build.stage === 'COMPLETED' &&
            candidate?.admissionVerdict === 'ACCEPTED' &&
            !candidate.promotionRequested &&
            !currentBuildPromoted
          "
        >
          {{ $t("assistant.roleImageBuild.awaitingPromotion") }}
        </p>
        <p v-if="awaitingAdmission">
          {{ $t("assistant.roleImageBuild.admissionPending") }}
        </p>
        <p v-if="currentBuildPromoted">
          {{ $t("assistant.roleImageBuild.ready") }}
        </p>
        <div class="assistant-build-card__state">
          <span>{{ $t("roleImages.admissionVerdict") }}</span>
          <StatusBadge
            :state="
              candidate?.admissionVerdict ??
              (currentBuildPromoted
                ? detail.activeArtifact?.admissionVerdict
                : undefined) ??
              'PENDING'
            "
          />
        </div>
        <p
          v-if="admissionTimedOut && !candidate && !currentBuildPromoted"
          class="assistant-build-card__problem"
          role="alert"
        >
          {{ $t("assistant.roleImageBuild.admissionUnknown") }}
        </p>
        <div class="assistant-build-card__state">
          <span>{{ $t("roleImages.promotion") }}</span>
          <StatusBadge :state="promotionState" />
        </div>
        <p v-if="candidate?.admissionVerdict === 'REJECTED'">
          {{ $t("assistant.roleImageBuild.admissionRejected") }}
        </p>
        <p v-if="promotionPending">
          {{ $t("assistant.roleImageBuild.promotionPending") }}
        </p>
        <p
          v-if="promotionFailed"
          class="assistant-build-card__problem"
          role="alert"
        >
          {{ $t("assistant.roleImageBuild.promotionFailed") }}
        </p>
        <p
          v-if="(promotionProblem || promotionTimedOut) && !promotionFailed"
          class="assistant-build-card__problem"
          role="alert"
        >
          {{ $t("assistant.roleImageBuild.promotionUnknown") }}
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
          query: { assistantForm: '1' },
        }"
        @click="emit('navigate')"
      >
        {{ $t("assistant.roleImageBuild.open") }}
      </RouterLink>
      <button
        v-if="debuggableFailure"
        class="button button--primary"
        type="button"
        :disabled="loading || stopping"
        @click="requestDebug"
      >
        {{ $t("assistant.roleImageBuild.debug") }}
      </button>
      <button
        v-if="
          candidate &&
          detail &&
          canPromoteRoleImage(detail.recipe, candidate) &&
          !currentBuildPromoted &&
          attemptedArtifactRef !== candidate.ref
        "
        class="button button--primary"
        type="button"
        :disabled="promoting || loading || stopping"
        @click="promoteCandidate"
      >
        {{ $t("assistant.roleImageBuild.promote") }}
      </button>
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
.assistant-build-card__actions,
.assistant-build-card__state {
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
.assistant-build-card__diagnostics {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.assistant-build-card__diagnostics div {
  display: grid;
  grid-template-columns: minmax(120px, 0.35fr) minmax(0, 1fr);
  gap: 8px;
}
.assistant-build-card__diagnostics dt,
.assistant-build-card__diagnostics dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-build-card__diagnostics dt {
  color: var(--muted);
}
</style>
