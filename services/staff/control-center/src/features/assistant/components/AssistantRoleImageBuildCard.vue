<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { assistantRoleImageBuildTarget } from "@/features/assistant/model";
import { usePlatformStore } from "@/features/platform/store";
import RoleImageAdmissionFailureNotice from "@/features/role-images/RoleImageAdmissionFailureNotice.vue";
import {
  currentRoleImageAdmissionFailure,
  assertRoleImageAdmissionFailure,
} from "@/features/role-images/admission-failure";
import {
  commandRoleImage,
  loadRoleImageDetail,
  promoteRoleImageArtifact,
} from "@/features/role-images/api";
import {
  buildIsActive,
  canPromoteRoleImage,
  latestBuild,
} from "@/features/role-images/model";
import type {
  AssistantPlan,
  RoleImagePromotionReceipt,
  RoleImageRecipeDetail,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import { requestConfirmation } from "@/shared/ui/confirmation";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: []; debug: [prompt: string] }>();
const { t } = useI18n();
const platform = usePlatformStore();
const localizeServerMessage = useServerMessage();
const target = computed(() =>
  assistantRoleImageBuildTarget(
    props.plan,
    props.operationRef,
    platform.bootstrap?.organizationRef,
  ),
);
const resourceAddress = computed(
  () => target.value?.resourceScope ?? target.value?.projectRef,
);
const targetRoute = computed(() =>
  target.value?.resourceScope
    ? {
        name: "system-role-image",
        params: { recipeRef: target.value.recipeRef },
        query: { assistantForm: "1" },
      }
    : {
        name: "role-image",
        params: {
          projectRef: target.value?.projectRef,
          recipeRef: target.value?.recipeRef,
        },
        query: { assistantForm: "1" },
      },
);
const detail = ref<RoleImageRecipeDetail>();
const loading = ref(false);
const stopping = ref(false);
const promoting = ref(false);
const problem = ref(false);
const promotionProblem = ref(false);
const promotionReceipt = ref<RoleImagePromotionReceipt>();
const attemptedArtifactRef = ref<string>();
const build = computed(() => latestBuild(detail.value?.builds ?? []));
const admissionFailure = computed(() =>
  currentRoleImageAdmissionFailure(
    detail.value?.recipe,
    build.value,
    detail.value?.admissionFailure,
  ),
);
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
    ((candidate.value?.promotionRequested === true &&
      ["PENDING", "CLAIMED", "AUTHORIZED"].includes(
        candidate.value.promotionState,
      )) ||
      ["QUEUED", "PROMOTING"].includes(promotionReceipt.value?.state ?? "")),
);
const promotionState = computed(() =>
  admissionFailure.value
    ? "FAILED"
    : currentBuildPromoted.value
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
    !admissionFailure.value &&
    !currentBuildPromoted.value,
);
const cancellable = computed(() => build.value && buildIsActive(build.value));
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
    promotionProblem.value = false;
    promotionReceipt.value = undefined;
    attemptedArtifactRef.value = undefined;
    loading.value = false;
    if (!value) return;
    const controller = new AbortController();
    let refreshRequested = false;
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      if (loading.value) {
        refreshRequested = true;
        return;
      }
      loading.value = true;
      try {
        const next = await loadRoleImageDetail(
          value.resourceScope ?? value.projectRef,
          value.recipeRef,
          controller.signal,
        );
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (
          next.recipe.ref !== value.recipeRef ||
          (value.resourceScope
            ? next.recipe.scopeKind !== "ORGANIZATION" ||
              next.recipe.organizationRef !==
                value.resourceScope.organizationRef ||
              next.recipe.projectRef !== ""
            : next.recipe.projectRef !== value.projectRef) ||
          next.builds.some((item) => item.recipeRef !== value.recipeRef)
        )
          throw new Error("Role image build scope mismatch");
        assertRoleImageAdmissionFailure(next);
        detail.value = next;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) {
          loading.value = false;
          if (refreshRequested) {
            refreshRequested = false;
            void refresh?.();
          }
        }
      }
    };
    void refresh();
  },
  { immediate: true },
);

watch(
  [
    () => platform.roleImageRealtimeRevision,
    () => platform.organizationRoleImageRealtimeRevision,
  ],
  (
    [projectRevision, organizationRevision],
    [previousProject, previousOrganization],
  ) => {
    const exact = target.value;
    if (
      exact &&
      (exact.resourceScope
        ? organizationRevision !== previousOrganization
        : projectRevision !== previousProject)
    )
      void refresh?.();
  },
);

async function stopBuild(): Promise<void> {
  const current = detail.value?.recipe;
  const exact = target.value;
  if (
    !current ||
    !exact ||
    !resourceAddress.value ||
    !cancellable.value ||
    !build.value ||
    !current.nextActions.includes("CANCEL_BUILD") ||
    stopping.value ||
    !(await requestConfirmation({
      message: t("assistant.roleImageBuild.stopConfirm"),
      tone: "danger",
    }))
  )
    return;
  if (target.value !== exact) return;
  stopping.value = true;
  try {
    await commandRoleImage(
      exact.resourceScope ?? exact.projectRef,
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
    !resourceAddress.value ||
    !recipe ||
    !artifact ||
    !canPromoteRoleImage(recipe, artifact) ||
    promoting.value ||
    loading.value ||
    attemptedArtifactRef.value === artifact.ref ||
    !(await requestConfirmation(t("assistant.roleImageBuild.promoteConfirm")))
  )
    return;
  if (target.value !== exact) return;
  // После неопределённого ответа нельзя повторять state-changing command.
  attemptedArtifactRef.value = artifact.ref;
  promoting.value = true;
  promotionProblem.value = false;
  try {
    const receipt = await promoteRoleImageArtifact(
      exact.resourceScope ?? exact.projectRef,
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
      <p>{{ localizeServerMessage(detail.recipe.name) }}</p>
      <RoleImageAdmissionFailureNotice
        v-if="admissionFailure"
        :failure="admissionFailure"
      />
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
        <p v-if="['FAILED', 'EXPIRED'].includes(build.stage)">
          {{ $t("roleImages.retryPending") }}
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
              admissionFailure
                ? 'FAILED'
                : (candidate?.admissionVerdict ??
                  (currentBuildPromoted
                    ? detail.activeArtifact?.admissionVerdict
                    : undefined) ??
                  'PENDING')
            "
          />
        </div>
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
          v-if="promotionProblem && !promotionFailed"
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
      <RouterLink class="button" :to="targetRoute" @click="emit('navigate')">
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
