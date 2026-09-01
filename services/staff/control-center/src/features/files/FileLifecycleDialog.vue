<script setup lang="ts">
import { AlertTriangle, RotateCcw, Trash2 } from "@lucide/vue";
import { computed } from "vue";
import { RouterLink } from "vue-router";

import type {
  ArtifactLifecycleAction,
  ArtifactLifecycleBlockReason,
  ArtifactLifecycleState,
} from "@/features/files/model";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

const props = defineProps<{
  action: ArtifactLifecycleAction;
  artifact: Artifact;
  busy?: boolean;
  labels: {
    cancel: string;
    confirm: Record<ArtifactLifecycleAction, string>;
    description: Record<ArtifactLifecycleAction, string>;
    impact: {
      activeRuns: string;
      activeRunsTruncated: string;
      attachments: string;
      bindings: string;
      openRun: string;
      summary: string;
    };
    impactBlocked: string;
    impactUnavailable: string;
    reason: Record<ArtifactLifecycleBlockReason, string>;
    title: Record<ArtifactLifecycleAction, string>;
  };
  state: ArtifactLifecycleState;
}>();

const emit = defineEmits<{ close: []; confirm: [] }>();
const destructive = computed(() => props.action !== "RESTORE");

function runPath(run: { projectRef?: string; runRef: string }): string {
  return run.projectRef
    ? `/projects/${encodeURIComponent(run.projectRef)}/runs/${encodeURIComponent(run.runRef)}`
    : `/runs/${encodeURIComponent(run.runRef)}`;
}
</script>

<template>
  <ModalDialog
    :title="labels.title[action]"
    :busy="busy"
    size="sm"
    @close="emit('close')"
  >
    <div class="file-lifecycle-dialog">
      <span
        class="file-lifecycle-dialog__icon"
        :class="{ 'file-lifecycle-dialog__icon--danger': destructive }"
        aria-hidden="true"
      >
        <RotateCcw v-if="action === 'RESTORE'" :size="22" />
        <Trash2 v-else :size="22" />
      </span>
      <div>
        <p>
          <strong>{{ artifact.fileName }}</strong>
        </p>
        <p>{{ labels.description[action] }}</p>
      </div>
      <div
        v-if="!state.available"
        class="file-lifecycle-dialog__notice"
        role="status"
      >
        <AlertTriangle :size="18" aria-hidden="true" />
        <div>
          <strong>{{ labels.reason[state.reason] }}</strong>
          <p>
            {{
              state.reason === "IMPACT_UNAVAILABLE"
                ? labels.impactUnavailable
                : labels.impactBlocked
            }}
          </p>
        </div>
      </div>
      <section v-if="state.impact" class="file-lifecycle-dialog__impact">
        <strong>{{ labels.impact.summary }}</strong>
        <dl>
          <div>
            <dt>{{ labels.impact.bindings }}</dt>
            <dd>{{ state.impact.bindingCount }}</dd>
          </div>
          <div>
            <dt>{{ labels.impact.attachments }}</dt>
            <dd>{{ state.impact.attachmentCount }}</dd>
          </div>
          <div>
            <dt>{{ labels.impact.activeRuns }}</dt>
            <dd>{{ state.impact.activeRuntimeCount }}</dd>
          </div>
        </dl>
        <ul v-if="state.impact.activeRuns.length > 0">
          <li v-for="run in state.impact.activeRuns" :key="run.runRef">
            <div>
              <strong>{{ run.title }}</strong>
              <small>{{ run.state }}</small>
            </div>
            <RouterLink class="button" :to="runPath(run)">
              {{ labels.impact.openRun }}
            </RouterLink>
          </li>
        </ul>
        <p
          v-if="state.impact.activeRunsTruncated"
          class="file-lifecycle-dialog__truncated"
        >
          {{ labels.impact.activeRunsTruncated }}
        </p>
      </section>
    </div>

    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ labels.cancel }}
      </button>
      <button
        class="button"
        :class="destructive ? 'button--danger' : 'button--primary'"
        type="button"
        :disabled="busy || !state.available"
        :title="state.available ? undefined : labels.reason[state.reason]"
        @click="emit('confirm')"
      >
        <RotateCcw v-if="action === 'RESTORE'" :size="16" aria-hidden="true" />
        <Trash2 v-else :size="16" aria-hidden="true" />
        {{ labels.confirm[action] }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.file-lifecycle-dialog {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}
.file-lifecycle-dialog__icon {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  border-radius: 7px;
  background: var(--accent-soft);
  color: var(--accent-strong);
}
.file-lifecycle-dialog__icon--danger {
  background: var(--danger-soft, #fff0f0);
  color: var(--danger, #b42318);
}
.file-lifecycle-dialog p {
  margin: 0 0 8px;
  color: var(--muted);
  line-height: 1.45;
}
.file-lifecycle-dialog strong {
  color: var(--text);
  overflow-wrap: anywhere;
}
.file-lifecycle-dialog__notice {
  display: flex;
  grid-column: 1 / -1;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--warning-border, #e8c46a);
  border-radius: 7px;
  background: var(--warning-soft, #fff8e6);
}
.file-lifecycle-dialog__notice svg {
  flex: 0 0 auto;
  color: var(--warning, #8a6100);
}
.file-lifecycle-dialog__notice p {
  margin: 4px 0 0;
  font-size: 0.82rem;
}
.file-lifecycle-dialog__impact {
  display: grid;
  grid-column: 1 / -1;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.file-lifecycle-dialog__impact dl {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.file-lifecycle-dialog__impact dl div {
  padding: 8px;
  border-radius: 6px;
  background: var(--surface-muted, #f5f7fa);
}
.file-lifecycle-dialog__impact dt,
.file-lifecycle-dialog__impact dd {
  margin: 0;
}
.file-lifecycle-dialog__impact dt,
.file-lifecycle-dialog__impact small {
  color: var(--muted);
  font-size: 0.78rem;
}
.file-lifecycle-dialog__impact ul {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.file-lifecycle-dialog__impact li {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.file-lifecycle-dialog__impact li div {
  display: grid;
  min-width: 0;
}
.file-lifecycle-dialog__truncated {
  margin: 0;
}
@media (max-width: 640px) {
  .file-lifecycle-dialog__impact dl {
    grid-template-columns: 1fr;
  }
}
</style>
