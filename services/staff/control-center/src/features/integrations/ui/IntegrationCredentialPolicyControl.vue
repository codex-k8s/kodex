<script setup lang="ts">
import { useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  credentialPolicySupported,
  useCredentialPolicy,
} from "../credential-policy";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{
  connection: IntegrationConnection;
  definition?: IntegrationDefinition;
  blocked?: boolean;
}>();
const emit = defineEmits<{ changed: [] }>();
const { t } = useI18n();
const fieldId = useId();
const model = useCredentialPolicy(
  () => props.definition,
  () => emit("changed"),
);
watch(
  () => [
    props.connection.ref,
    props.connection.version,
    props.definition?.digest,
  ],
  () => model.close(),
  { flush: "sync" },
);
const {
  opened,
  connection: current,
  selection,
  busy,
  problem,
  requiresRefresh,
  saved,
  allowed,
} = model;
</script>

<template>
  <div class="credential-policy-control">
    <small>{{
      t(`credentialPolicy.modes.${connection.credentialPolicy}`)
    }}</small>
    <button
      v-if="credentialPolicySupported(connection, definition)"
      class="button button--small"
      type="button"
      :disabled="props.blocked || busy"
      @click="model.open(connection)"
    >
      {{ t("credentialPolicy.choose") }}
    </button>
    <ModalDialog
      v-if="opened"
      :title="t('credentialPolicy.title')"
      :busy="busy"
      size="sm"
      @close="model.close()"
    >
      <form class="credential-policy-form" @submit.prevent="model.save()">
        <p>{{ current?.name ?? connection.name }}</p>
        <p>{{ t("credentialPolicy.description") }}</p>
        <p v-if="!allowed && !busy && !requiresRefresh" role="status">
          {{ t("credentialPolicy.idleRequired") }}
        </p>
        <p v-if="busy" role="status">{{ t("credentialPolicy.loading") }}</p>
        <ProblemNotice v-if="problem" :problem="problem" />
        <p v-if="requiresRefresh && !busy" role="alert">
          {{ t("credentialPolicy.refreshRequired") }}
        </p>
        <p v-if="saved" role="status">{{ t("credentialPolicy.saved") }}</p>
        <label :for="fieldId">{{ t("credentialPolicy.title") }}</label>
        <select
          :id="fieldId"
          v-model="selection"
          :disabled="!allowed || props.blocked"
        >
          <option value="PROJECT_SHARED">
            {{ t("credentialPolicy.modes.PROJECT_SHARED") }}
          </option>
          <option value="INITIATOR_REQUIRED">
            {{ t("credentialPolicy.modes.INITIATOR_REQUIRED") }}
          </option>
        </select>
        <p>
          {{
            t(
              selection === "INITIATOR_REQUIRED"
                ? "credentialPolicy.personalHelp"
                : "credentialPolicy.sharedHelp",
            )
          }}
        </p>
        <a
          v-if="current?.credentialPolicy === 'INITIATOR_REQUIRED'"
          href="/integrations?section=personal"
          >{{ t("credentialPolicy.myConnections") }}</a
        >
        <div class="dialog-actions">
          <button
            class="button"
            type="button"
            :disabled="busy"
            @click="model.refresh()"
          >
            {{ t("credentialPolicy.refresh") }}
          </button>
          <button
            class="button button--primary"
            type="submit"
            :disabled="
              !allowed ||
              props.blocked ||
              selection === current?.credentialPolicy
            "
          >
            {{ t("credentialPolicy.save") }}
          </button>
        </div>
      </form>
    </ModalDialog>
  </div>
</template>

<style scoped>
.credential-policy-control,
.credential-policy-form {
  display: grid;
  gap: var(--space-2);
}
.credential-policy-control > button {
  justify-self: start;
}
</style>
