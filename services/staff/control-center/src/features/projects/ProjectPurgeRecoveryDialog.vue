<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from "vue";
import { AlertTriangle, CheckCircle2, RefreshCw } from "@lucide/vue";
import { usePlatformStore } from "@/features/platform/store";
import { useProjectPurgeRecoveryStore } from "./purge-recovery-store";
import { purgeRecoveryStatusKey } from "./purge-recovery";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{ projectRef: string; projectName: string }>();
const emit = defineEmits<{ close: []; resolved: [] }>();
const platform = usePlatformStore();
const recovery = useProjectPurgeRecoveryStore();
const statusKey = computed(() =>
  recovery.preview ? purgeRecoveryStatusKey(recovery.preview) : "",
);

watch(
  () => props.projectRef,
  (refValue) => {
    void recovery.load(refValue);
  },
  { immediate: true },
);
const unsubscribe = platform.$onAction(({ name }) => {
  if (name === "clearOwnerState") {
    recovery.clearOwner();
    emit("close");
  }
});
watch(
  () => platform.bootstrap?.organizationRef,
  (next, previous) => {
    if (next !== previous) {
      recovery.clearOwner();
      emit("close");
    }
  },
  { flush: "sync" },
);
watch(
  () => platform.trashedProjects[props.projectRef]?.version,
  (next, previous) => {
    if (previous !== undefined && next !== previous && !recovery.submitting)
      void recovery.load(props.projectRef, true);
  },
);
async function resolve(): Promise<void> {
  if (await recovery.resolve()) emit("resolved");
}
onBeforeUnmount(() => {
  unsubscribe();
  recovery.close();
});
</script>

<template>
  <ModalDialog
    :title="$t('projects.recovery.title')"
    size="md"
    @close="emit('close')"
  >
    <div class="purge-recovery">
      <strong class="purge-recovery__name">{{ projectName }}</strong>
      <a
        :href="`/projects?trash=1&purgeRecovery=${encodeURIComponent(projectRef)}`"
        class="purge-recovery__link"
        >{{ $t("projects.recovery.statusLink") }}</a
      >
      <p v-if="recovery.loading" role="status">{{ $t("common.loading") }}</p>
      <ProblemNotice
        v-if="recovery.problem"
        :problem="recovery.problem"
        compact
      />
      <p v-if="recovery.stale" class="purge-recovery__notice" role="status">
        {{ $t("projects.recovery.stale") }}
      </p>
      <p v-if="recovery.uncertain" class="purge-recovery__notice" role="status">
        {{ $t("projects.recovery.uncertain") }}
      </p>
      <template v-if="recovery.preview">
        <p class="purge-recovery__status" role="status">
          <CheckCircle2
            v-if="
              recovery.preview.state === 'DELETED_CONFIRMED' &&
              recovery.preview.cleanupPendingCount === 0
            "
            :size="18"
            aria-hidden="true"
          />
          <AlertTriangle v-else :size="18" aria-hidden="true" />
          {{ $t(statusKey, { count: recovery.preview.cleanupPendingCount }) }}
        </p>
        <p v-if="recovery.preview.canResolve">
          {{
            $t(
              recovery.preview.unknownContentCount > 0
                ? "projects.recovery.reason"
                : "projects.recovery.retryReason",
              { count: recovery.preview.unknownContentCount },
            )
          }}
        </p>
        <div v-if="recovery.preview.canResolve" class="purge-recovery__warning">
          <p>
            {{
              $t(
                recovery.preview.unknownContentCount > 0
                  ? "projects.recovery.irreversible"
                  : "projects.recovery.retryWarning",
              )
            }}
          </p>
          <p>{{ $t("projects.recovery.observation") }}</p>
        </div>
        <label v-if="recovery.preview.canResolve" class="purge-recovery__ack">
          <input
            v-model="recovery.acknowledged"
            type="checkbox"
            :disabled="recovery.loading || recovery.submitting"
          />
          <span>{{
            $t(
              recovery.preview.unknownContentCount > 0
                ? "projects.recovery.acknowledge"
                : "projects.recovery.acknowledgeRetry",
            )
          }}</span>
        </label>
      </template>
    </div>
    <template #actions>
      <button class="button" type="button" @click="emit('close')">
        {{ $t("common.close") }}
      </button>
      <button
        class="button"
        type="button"
        :disabled="recovery.loading || recovery.submitting"
        @click="recovery.load(projectRef)"
      >
        <RefreshCw :size="16" aria-hidden="true" />{{
          $t("projects.recovery.refresh")
        }}
      </button>
      <button
        v-if="recovery.preview?.canResolve"
        class="button button--danger"
        type="button"
        :disabled="!recovery.canSubmit"
        @click="resolve"
      >
        {{
          $t(
            recovery.submitting
              ? "projects.recovery.submitting"
              : "projects.recovery.continue",
          )
        }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.purge-recovery {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.purge-recovery p {
  margin: 0;
}
.purge-recovery__name {
  overflow-wrap: anywhere;
}
.purge-recovery__status {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.purge-recovery__status svg {
  flex: 0 0 auto;
  margin-top: 2px;
}
.purge-recovery__warning {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.purge-recovery__ack {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  line-height: 1.5;
}
.purge-recovery__ack input {
  flex: 0 0 auto;
  margin-top: 4px;
}
.purge-recovery__notice {
  color: var(--muted);
}
</style>
