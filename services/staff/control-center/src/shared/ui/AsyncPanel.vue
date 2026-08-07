<script setup lang="ts">
import {
  AlertCircle,
  Ban,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
} from "@lucide/vue";

import type { AppProblem } from "@/shared/api/problem";
import type { RemotePhase } from "@/shared/lib/remote";

defineProps<{ phase: RemotePhase; problem?: AppProblem | null }>();
defineEmits<{ retry: [] }>();
</script>

<template>
  <div
    v-if="phase === 'loading' || phase === 'idle'"
    class="state-panel"
    role="status"
    aria-live="polite"
  >
    <LoaderCircle class="spin" :size="22" aria-hidden="true" />
    <span>{{ $t("common.loading") }}</span>
  </div>
  <div v-else-if="phase === 'empty'" class="state-panel state-panel--quiet">
    <slot name="empty">
      <span>{{ $t("common.empty") }}</span>
    </slot>
  </div>
  <div
    v-else-if="phase === 'forbidden'"
    class="state-panel state-panel--danger"
    role="alert"
  >
    <Ban :size="22" aria-hidden="true" />
    <strong>{{ $t("common.forbidden") }}</strong>
    <button
      class="button button--secondary"
      type="button"
      @click="$emit('retry')"
    >
      <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.retry") }}
    </button>
  </div>
  <div
    v-else-if="phase === 'conflict'"
    class="state-panel state-panel--warning"
    role="alert"
  >
    <RotateCcw :size="22" aria-hidden="true" />
    <div>
      <strong>{{ $t("common.conflictTitle") }}</strong
      ><br />{{ $t("common.conflictText") }}
    </div>
    <button
      class="button button--secondary"
      type="button"
      @click="$emit('retry')"
    >
      <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
    </button>
  </div>
  <div
    v-else-if="phase === 'error'"
    class="state-panel state-panel--danger"
    role="alert"
  >
    <AlertCircle :size="22" aria-hidden="true" />
    <div>
      <strong>{{ $t("common.error") }}</strong>
      <div v-if="problem" class="state-panel__meta">
        {{ $t("common.errorCode", { code: problem.code }) }}
        <span v-if="problem.correlationId">
          · {{ $t("common.correlation", { id: problem.correlationId }) }}</span
        >
      </div>
    </div>
    <button
      class="button button--secondary"
      type="button"
      @click="$emit('retry')"
    >
      <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.retry") }}
    </button>
  </div>
  <slot v-else />
</template>
