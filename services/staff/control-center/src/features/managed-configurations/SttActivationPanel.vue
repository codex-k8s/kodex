<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
  SystemSttConfiguration,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { idempotencyKey } from "@/shared/api/mutation";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  readPublicationAttempt,
  rememberPublicationAttempt,
  forgetPublicationAttempt,
} from "@/features/runtime/publication-attempt";
import {
  prepareSttActivation,
  activateStt,
  readSttStatus,
  type SttActivationPlan,
} from "./stt-activation";
import { sttActivationMessages } from "./stt-activation-messages";

const props = defineProps<{
  configuration: ManagedConfiguration;
  revision: ManagedConfigurationRevision;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
const { t } = useI18n({ useScope: "local", messages: sttActivationMessages });
const plan = ref<SttActivationPlan>();
const effective = ref<SystemSttConfiguration>();
const effectiveName = ref<string>();
const read = ref(false),
  busy = ref(false),
  unknown = ref(false),
  observed = ref(false),
  acknowledged = ref(false);
const problem = ref<AppProblem>();
let generation = 0;
let active: AbortController | undefined;
const owner = ownerRequestSignal();
const bindingRef = "stt-tts-service";
const allowed = computed(
  () =>
    props.configuration.kind === "SYSTEM_STT" &&
    props.configuration.managedBy === "UI" &&
    !props.configuration.projectRef &&
    props.revision.state === "PUBLISHED" &&
    !props.disabled &&
    !owner.aborted,
);
const already = computed(
  () =>
    plan.value?.current?.configurationRef === props.configuration.ref &&
    plan.value.current.revisionRef === props.revision.ref,
);
const selectedActive = computed(
  () =>
    read.value &&
    effective.value?.configurationRef === props.configuration.ref &&
    effective.value.revisionRef === props.revision.ref,
);
function invalidate() {
  active?.abort();
  generation++;
  plan.value = undefined;
  effective.value = undefined;
  effectiveName.value = undefined;
  read.value = false;
  problem.value = undefined;
  busy.value = false;
  emit("busy", false);
}
function restore() {
  try {
    unknown.value = !!readPublicationAttempt(
      "SYSTEM_STT",
      bindingRef,
      window.sessionStorage,
    );
  } catch (error) {
    unknown.value = true;
    problem.value = asProblem(error);
  }
}
watch(
  () => [
    props.configuration.ref,
    props.configuration.version,
    props.configuration.managedBy,
    props.configuration.projectRef,
    props.revision.ref,
    props.revision.state,
  ],
  () => {
    invalidate();
    acknowledged.value = false;
    observed.value = false;
    restore();
  },
  { immediate: true, flush: "sync" },
);
watch(
  () => props.disabled,
  (disabled) => {
    if (disabled && !busy.value) plan.value = undefined;
  },
);
owner.addEventListener("abort", invalidate, { once: true });
onBeforeUnmount(() => {
  invalidate();
  owner.removeEventListener("abort", invalidate);
});
async function work(operation: (signal: AbortSignal) => Promise<void>) {
  if (busy.value || owner.aborted) return;
  active?.abort();
  active = new AbortController();
  const signal = AbortSignal.any([active.signal, owner]);
  const turn = ++generation;
  busy.value = true;
  emit("busy", true);
  problem.value = undefined;
  try {
    await operation(signal);
  } catch (error) {
    if (!signal.aborted && turn === generation)
      problem.value = asProblem(error);
  } finally {
    if (turn === generation) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
function prepare() {
  if (!allowed.value || unknown.value || selectedActive.value) return;
  plan.value = undefined;
  read.value = false;
  void work(async (signal) => {
    const result = await prepareSttActivation(
      props.configuration,
      props.revision,
      signal,
    );
    if (!signal.aborted) {
      plan.value = result;
      effective.value = result.current;
      effectiveName.value = result.currentName;
      read.value = true;
    }
  });
}
async function reread(signal: AbortSignal) {
  read.value = false;
  const status = await readSttStatus(signal);
  const value = status.effective;
  if (signal.aborted) return;
  effective.value = value;
  effectiveName.value = status.name;
  read.value = true;
  const intent = readPublicationAttempt(
    "SYSTEM_STT",
    bindingRef,
    window.sessionStorage,
  );
  if (
    intent &&
    value?.revisionRef === intent.planRef &&
    value.configurationRef === intent.selectedItemRefs[0]
  ) {
    forgetPublicationAttempt("SYSTEM_STT", bindingRef, window.sessionStorage);
    unknown.value = false;
    observed.value = true;
  }
}
function confirm() {
  const selected = plan.value;
  if (!selected || !allowed.value || unknown.value || already.value) return;
  void work(async (signal) => {
    const key = idempotencyKey();
    rememberPublicationAttempt(
      {
        kind: "SYSTEM_STT",
        ownerRef: bindingRef,
        planRef: selected.revision.ref,
        version: selected.configuration.version,
        selectedItemRefs: [selected.configuration.ref],
        key,
      },
      window.sessionStorage,
    );
    unknown.value = true;
    plan.value = undefined;
    read.value = false;
    effective.value = undefined;
    try {
      await activateStt(selected, key, signal);
      if (signal.aborted) return;
      forgetPublicationAttempt("SYSTEM_STT", bindingRef, window.sessionStorage);
      unknown.value = false;
      acknowledged.value = true;
    } catch (error) {
      if (
        !signal.aborted &&
        [400, 401, 403, 404, 409, 412, 422].includes(asProblem(error).status)
      ) {
        forgetPublicationAttempt(
          "SYSTEM_STT",
          bindingRef,
          window.sessionStorage,
        );
        unknown.value = false;
      }
      throw error;
    }
    await reread(signal);
  });
}
onMounted(() => {
  void work(reread);
});
</script>
<template>
  <section class="stt-activation" :aria-busy="busy">
    <div class="stt-activation__header">
      <div class="stt-activation__heading">
        <h3>{{ t("activation.title") }}</h3>
        <p>{{ t("activation.intro") }}</p>
      </div>
      <p
        v-if="read"
        class="stt-activation__readiness"
        :class="{ 'stt-activation__readiness--ready': effective?.ready }"
        role="status"
        data-testid="stt-readiness"
      >
        {{
          t(
            effective
              ? effective.ready
                ? "activation.ready"
                : "activation.notReady"
              : "activation.absent",
          )
        }}
      </p>
    </div>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <p v-if="problem?.status === 412" role="status">
      {{ t("activation.stale") }}
    </p>
    <p v-if="unknown" role="alert">{{ t("activation.unknown") }}</p>
    <p v-if="acknowledged" role="status">{{ t("activation.acknowledged") }}</p>
    <p v-if="observed" role="status">{{ t("activation.observed") }}</p>
    <p v-if="read && effective" class="stt-activation__current">
      {{
        t("activation.active", {
          name: effectiveName,
          revision: effective.revision,
        })
      }}
    </p>
    <template v-if="plan">
      <p>
        {{
          plan.current
            ? t("activation.change", {
                name: plan.currentName,
                revision: plan.current.revision,
              })
            : t("activation.first")
        }}
      </p>
      <p>
        {{
          t("activation.target", {
            name: plan.configuration.name,
            revision: plan.revision.revision,
          })
        }}
      </p>
      <p v-if="already">{{ t("activation.already") }}</p>
    </template>
    <div class="stt-activation__actions">
      <button
        v-if="plan"
        class="button button--primary"
        :disabled="busy || !allowed || already || unknown"
        @click="confirm"
      >
        {{ t("activation.confirm") }}
      </button>
      <button
        v-if="plan"
        class="button"
        :disabled="busy"
        @click="plan = undefined"
      >
        {{ t("activation.cancel") }}
      </button>
      <button
        v-if="!plan && !selectedActive"
        class="button"
        :disabled="busy || !allowed || unknown"
        @click="prepare"
      >
        {{ t("activation.prepare") }}
      </button>
      <button
        class="button"
        :disabled="busy || owner.aborted"
        @click="work(reread)"
      >
        {{ t("activation.readback") }}
      </button>
    </div>
  </section>
</template>
<style scoped>
.stt-activation {
  display: grid;
  gap: 10px;
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 16px;
  overflow-wrap: anywhere;
}
.stt-activation__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.stt-activation__heading {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.stt-activation__heading h3,
.stt-activation p {
  margin: 0;
}
.stt-activation__heading p,
.stt-activation__current {
  color: var(--text-muted);
}
.stt-activation__readiness {
  flex: 0 1 460px;
  padding: 6px 10px;
  border-radius: 8px;
  background: var(--surface-muted, #f2f5f8);
  font-size: 12px;
}
.stt-activation__readiness--ready {
  color: var(--success, #18803c);
  background: var(--success-subtle, #eaf6ef);
}
.stt-activation__actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.stt-activation__actions .button {
  max-width: 100%;
}
@media (max-width: 900px) {
  .stt-activation__header {
    flex-direction: column;
  }
}
</style>
