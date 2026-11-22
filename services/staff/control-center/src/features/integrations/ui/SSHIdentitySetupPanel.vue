<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useSSHIdentitySetup } from "../ssh-identity-setup";
import type { SSHSetupContext } from "../ssh-identity-setup-api";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import IntegrationCredentialField from "./IntegrationCredentialField.vue";

const props = defineProps<{ context: SSHSetupContext; disabled?: boolean }>();
const emit = defineEmits<{ changed: [] }>();
const { t } = useI18n();
const prefix = useId();
const model = useSSHIdentitySetup(() => props.context);
const {
  privateKey,
  setup,
  busy,
  problem,
  needsReadback,
  pendingBeginKey,
  unavailable,
} = model;
const statusKey = computed(
  () => `integrations.sshSetupStates.${setup.value?.state ?? "PREPARED"}`,
);
watch(
  () => JSON.stringify(props.context),
  () => model.resetContext(),
);
watch(
  () =>
    setup.value?.state === "COMPLETED"
      ? `${setup.value.ref}:${String(setup.value.version)}`
      : "",
  (value, previous) => {
    if (value && value !== previous) emit("changed");
  },
);
onMounted(() => window.addEventListener("pagehide", model.suspend));
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
</script>

<template>
  <section class="ssh-setup card" :aria-labelledby="`${prefix}-title`">
    <h3 :id="`${prefix}-title`">{{ t("integrations.sshSetupTitle") }}</h3>
    <p class="muted">{{ t("integrations.sshSetupBoundary") }}</p>
    <p>
      {{
        t(
          context.policy === "PROJECT_SHARED"
            ? "integrations.sshSetupShared"
            : "integrations.sshSetupPersonal",
        )
      }}
    </p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="setup" role="status">{{ t(statusKey) }}</p>
    <p v-if="needsReadback" class="muted">
      {{ t("integrations.sshSetupNeedsReadback") }}
    </p>
    <p v-if="unavailable">{{ t("integrations.sshSetupOwnerChanged") }}</p>
    <form @submit.prevent="model.submit()">
      <IntegrationCredentialField
        v-model="privateKey"
        multiline
        :disabled="disabled || busy || unavailable"
      />
      <button
        class="button button--primary"
        type="submit"
        :disabled="disabled || busy || unavailable || !privateKey"
      >
        {{ t("integrations.sshSetupUpload") }}
      </button>
    </form>
    <div v-if="setup || pendingBeginKey" class="ssh-setup-actions">
      <button
        class="button"
        type="button"
        :disabled="busy || unavailable"
        @click="model.refresh()"
      >
        {{ t("integrations.sshSetupRead") }}
      </button>
      <button
        v-if="model.canObserve()"
        class="button"
        type="button"
        :disabled="disabled || busy || unavailable"
        @click="model.observe()"
      >
        {{ t("integrations.sshSetupObserve") }}
      </button>
      <button
        v-if="
          setup && ['PREPARED', 'ATTEMPTING', 'UNKNOWN'].includes(setup.state)
        "
        class="button"
        type="button"
        :disabled="busy || unavailable"
        @click="model.cancel()"
      >
        {{ t("integrations.sshSetupCancel") }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.ssh-setup {
  display: grid;
  gap: 12px;
}
.ssh-setup h3,
.ssh-setup p {
  margin: 0;
}
.ssh-setup form {
  display: grid;
  gap: 12px;
}
.ssh-setup-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
