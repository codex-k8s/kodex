<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { copyConfiguration } from "./lifecycle";
import type { ConfigurationCopySource } from "./copy-source";

const props = defineProps<{ source: ConfigurationCopySource }>();
const emit = defineEmits<{
  close: [];
  created: [configuration: ManagedConfiguration];
}>();
const name = ref(props.source.name);
const busy = ref(false);
const submitted = ref(false);
const problem = ref<AppProblem>();
const unknown = ref(false);
const controller = new AbortController();
const owner = ownerRequestSignal();
onBeforeUnmount(() => controller.abort());
async function submit(): Promise<void> {
  if (submitted.value || busy.value || !name.value.trim()) return;
  submitted.value = true;
  busy.value = true;
  try {
    const result = await copyConfiguration(
      props.source,
      name.value.trim(),
      AbortSignal.any([controller.signal, owner]),
    );
    if (!controller.signal.aborted && !owner.aborted)
      emit("created", result.configuration);
  } catch (error) {
    if (!controller.signal.aborted && !owner.aborted) {
      problem.value = asProblem(error);
      unknown.value = problem.value.status === 0 || problem.value.status >= 500;
    }
  } finally {
    if (!controller.signal.aborted && !owner.aborted) busy.value = false;
  }
}
</script>

<template>
  <ModalDialog :title="$t('managed.copy')" :busy="busy" @close="emit('close')">
    <p>{{ $t("managed.copyConfirm") }}</p>
    <p v-if="source.origin === 'SHIPPED'">
      {{ $t("managed.shippedReadOnly") }}
    </p>
    <dl class="copy-source">
      <dt>{{ $t("managed.source") }}</dt>
      <dd>{{ source.origin }} · {{ source.ref }}</dd>
      <dt>{{ $t("managed.sourceRevision") }}</dt>
      <dd>{{ source.revision }} · v{{ source.version }}</dd>
      <template v-if="source.digest">
        <dt>{{ $t("managed.copyProvenance") }}</dt>
        <dd>{{ source.digest }}</dd>
      </template>
    </dl>
    <label
      >{{ $t("common.name")
      }}<input v-model="name" maxlength="160" :disabled="submitted"
    /></label>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="unknown" role="status">{{ $t("managed.outcomeUnknown") }}</p>
    <template #actions>
      <button
        class="button button--primary"
        :disabled="busy || submitted || !name.trim()"
        @click="submit"
      >
        {{ $t("managed.copy") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.copy-source dd {
  overflow-wrap: anywhere;
  margin-bottom: 10px;
}
</style>
