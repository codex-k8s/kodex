<script setup lang="ts">
import { onMounted, onBeforeUnmount, useId } from "vue";
import { useI18n } from "vue-i18n";
import type {
  EmailMailboxOwnership,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { useEmailMailboxOwnership } from "../personal-email-ownership";
const props = defineProps<{ connection: IntegrationConnection }>(),
  emit = defineEmits<{
    saved: [];
    ownership: [value: EmailMailboxOwnership | undefined];
  }>();
const { t } = useI18n(),
  prefix = useId(),
  model = useEmailMailboxOwnership(
    () => props.connection,
    (value) => emit("ownership", value),
    () => emit("saved"),
  );
const {
  ownership,
  problem,
  pending,
  policy,
  serviceSubjectRef,
  busy,
  loading,
  requiresRefresh,
  saved,
  canConfigure,
  canRevoke,
  canTest,
} = model;
onMounted(() => window.addEventListener("pagehide", model.suspend));
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
</script>
<template>
  <section class="stack" :aria-labelledby="`${prefix}-title`">
    <h3 :id="`${prefix}-title`">{{ t("personalEmail.ownerTitle") }}</h3>
    <p>{{ t("personalEmail.ownerHelp") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="loading" role="status">{{ t("personalEmail.loading") }}</p>
    <p v-if="requiresRefresh" role="status">
      {{ t("personalEmail.refreshRequired") }}
    </p>
    <p v-if="saved" role="status">{{ t("personalEmail.saved") }}</p>
    <button
      class="button"
      type="button"
      :disabled="busy || loading"
      @click="model.refresh()"
    >
      {{ t("personalEmail.refresh") }}
    </button>
    <div v-if="pending" role="alert" class="stack">
      <p>{{ t("personalEmail.pending") }}</p>
      <button
        class="button"
        type="button"
        :disabled="busy || loading"
        @click="model.recover()"
      >
        {{ t("personalEmail.recover") }}
      </button>
    </div>
    <template v-if="ownership"
      ><p>{{ t(`personalEmail.states.${ownership.state}`) }}</p>
      <p
        v-if="ownership.state === 'ACTIVE' && !ownership.ownerEligible"
        role="alert"
      >
        {{ t("personalEmail.ownerUnavailable") }}
      </p>
      <p>{{ t("personalEmail.idle") }}</p>
      <form class="stack" @submit.prevent="model.change('CONFIGURE')">
        <fieldset class="stack" :disabled="!canConfigure">
          <label class="field" :for="`${prefix}-policy`"
            ><span>{{ t("personalEmail.policy") }}</span
            ><select :id="`${prefix}-policy`" v-model="policy">
              <option value="INITIATOR_REQUIRED">
                {{ t("personalEmail.personal") }}
              </option>
              <option value="PROJECT_SHARED">
                {{ t("personalEmail.service") }}
              </option>
            </select></label
          >
          <label
            v-if="policy === 'PROJECT_SHARED'"
            class="field"
            :for="`${prefix}-service`"
            ><span>{{ t("personalEmail.serviceRef") }}</span
            ><input
              :id="`${prefix}-service`"
              v-model="serviceSubjectRef"
              maxlength="96"
              required
          /></label>
          <p v-if="policy === 'PROJECT_SHARED'" class="muted">
            {{ t("personalEmail.serviceHelp") }}
          </p>
          <button class="button" type="submit">
            {{ t("personalEmail.configure") }}
          </button>
        </fieldset>
      </form>
      <p class="muted">{{ t("personalEmail.revokeHelp") }}</p>
      <button
        class="button button--danger"
        type="button"
        :disabled="!canRevoke"
        @click="model.change('REVOKE')"
      >
        {{ t("personalEmail.revoke") }}
      </button>
      <p class="muted">{{ t("personalEmail.testHelp") }}</p>
      <button
        class="button"
        type="button"
        :disabled="!canTest"
        @click="model.test()"
      >
        {{ t("personalEmail.test") }}
      </button>
    </template>
  </section>
</template>
