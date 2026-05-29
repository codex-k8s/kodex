<script setup lang="ts">
import { useI18n } from 'vue-i18n';

import type { ApiError } from '@/shared/api/errors';

defineProps<{
  error?: ApiError;
  retryLabel?: string;
}>();

const emit = defineEmits<{
  retry: [];
}>();

const { t } = useI18n();
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal">
    <div class="api-error">
      <div>
        <div class="api-error__title">{{ t(error.messageKey) }}</div>
        <div class="api-error__meta">
          {{ t('errors.code') }}: {{ error.code }}
          <span v-if="error.requestId"> · {{ t('errors.requestId') }}: {{ error.requestId }}</span>
          <span v-if="error.correlationId"> · {{ t('errors.correlationId') }}: {{ error.correlationId }}</span>
        </div>
      </div>
      <v-btn
        v-if="retryLabel"
        color="error"
        variant="tonal"
        size="small"
        @click="emit('retry')"
      >
        {{ retryLabel }}
      </v-btn>
    </div>
  </v-alert>
</template>

<style scoped>
.api-error {
  align-items: flex-start;
  display: flex;
  gap: 12px;
  justify-content: space-between;
}

.api-error__title {
  font-weight: 700;
}

.api-error__meta {
  font-size: 0.8125rem;
  margin-top: 4px;
  opacity: 0.82;
}
</style>
