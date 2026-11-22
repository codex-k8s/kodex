<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import type {
  EmailMailboxOwnership,
  EmailMailboxSpecification,
  IntegrationConnection,
} from "../../src/shared/api/generated/openapi/types.gen";
import { getIntegrationConnection } from "../../src/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "../../src/shared/api/client";
import {
  unwrap,
  asProblem,
  type AppProblem,
} from "../../src/shared/api/problem";
import PersonalEmailConnectionsPanel from "../../src/features/integrations/ui/PersonalEmailConnectionsPanel.vue";
import EmailMailboxOwnershipPanel from "../../src/features/integrations/ui/EmailMailboxOwnershipPanel.vue";
import EmailMailboxCredentialPanel from "../../src/features/integrations/ui/EmailMailboxCredentialPanel.vue";
import EmailMailboxFields from "../../src/features/integrations/ui/EmailMailboxFields.vue";
import ProblemNotice from "../../src/shared/ui/ProblemNotice.vue";

// Только synthetic сборка: реальные feature components и generated HTTP client,
// ответы API задаёт browser fixture. Это не доказательство backend authorization.
const route = useRoute(),
  connection = ref<IntegrationConnection>(),
  ownership = ref<EmailMailboxOwnership>(),
  problem = ref<AppProblem>();
const specification = ref<EmailMailboxSpecification>({
  recipients: [],
  allowedFolders: ["INBOX", "Drafts", "Archive"],
  folder: "INBOX",
  draftsFolder: "Drafts",
  archiveFolder: "Archive",
});
const selected = computed(() =>
  typeof route.query.connectionRef === "string"
    ? route.query.connectionRef
    : "",
);
let generation = 0,
  controller: AbortController | undefined;
async function refresh() {
  const ref = selected.value;
  if (!ref) return;
  controller?.abort();
  controller = new AbortController();
  const epoch = ++generation;
  ownership.value = undefined;
  try {
    const value = (
      await unwrap(
        getIntegrationConnection({
          path: { connectionRef: ref },
          signal: requestSignal(controller.signal),
          cache: "no-store",
        }),
      )
    ).data;
    if (epoch !== generation) return;
    if (value.ref !== ref)
      throw new Error("Synthetic connection response is invalid");
    connection.value = value;
  } catch (error) {
    if (epoch === generation) problem.value = asProblem(error);
  }
}
watch(
  selected,
  () => {
    generation++;
    controller?.abort();
    connection.value = undefined;
    ownership.value = undefined;
    void refresh();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  generation++;
  controller?.abort();
});
</script>
<template>
  <main style="padding: 24px; max-width: 1000px; margin: auto" class="stack">
    <PersonalEmailConnectionsPanel />
    <ProblemNotice v-if="problem" :problem="problem" />
    <section
      v-if="connection"
      class="stack"
      aria-label="Настройка выбранного synthetic ящика"
    >
      <h2>{{ connection.name }}</h2>
      <EmailMailboxOwnershipPanel
        :connection="connection"
        @ownership="ownership = $event"
        @saved="refresh"
      />
      <EmailMailboxCredentialPanel
        v-if="ownership"
        :connection="connection"
        :ownership="ownership"
        @saved="refresh"
      />
      <template v-if="ownership?.ownerEligible"
        ><EmailMailboxFields
          :value="specification"
          :credentials="[]"
          @change="specification = $event"
        /><output aria-label="Синтетический почтовый черновик">{{
          JSON.stringify(specification)
        }}</output></template
      >
    </section>
  </main>
</template>
