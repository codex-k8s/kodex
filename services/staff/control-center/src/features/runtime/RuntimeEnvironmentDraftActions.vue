<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentDraftSpecification,
  RuntimeEnvironmentSet,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { idempotencyKey } from "@/shared/api/mutation";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import PublicationImpactSelection from "./PublicationImpactSelection.vue";
import {
  createEnvironmentDraft,
  environmentDraftFingerprint,
  prepareEnvironmentPublication,
  publishEnvironmentDraft,
  readEnvironmentDraft,
  saveEnvironmentDraft,
  transitionEnvironmentDraft,
} from "./environment-drafts";
import {
  assertRuntimeResourceAddressIdentity,
  runtimeResourceAddressKey,
  type RuntimeResourceAddress,
} from "./resource-scope";
import {
  forgetPublicationAttempt,
  readPublicationAttempt,
  rememberPublicationAttempt,
  type PublicationAttempt,
} from "./publication-attempt";
import {
  publicationPlanIdentity,
  restorePublicationImpact,
} from "./publication-impact";

const props = defineProps<{
  resourceScope: RuntimeResourceAddress;
  environment: RuntimeEnvironmentSet;
  specification: RuntimeEnvironmentDraftSpecification;
  canEdit: boolean;
  valid: boolean;
  initialDraftRef?: string;
  expectedDraftVersion?: number;
}>();
const emit = defineEmits<{
  draftLoaded: [draft: RuntimeEnvironmentDraft];
  draftSaved: [draft: RuntimeEnvironmentDraft];
  published: [environment: RuntimeEnvironmentSet];
  reauthenticate: [draft: RuntimeEnvironmentDraft];
  busy: [value: boolean];
}>();
const draft = ref<RuntimeEnvironmentDraft>();
const plan = ref<RevisionImpactPlan>();
const problem = ref<AppProblem>();
const busy = ref(false);
const pending = ref<PublicationAttempt>();
const saveAttempt = ref<{
  key: string;
  fingerprint: string;
  draft?: RuntimeEnvironmentDraft;
  specification: RuntimeEnvironmentDraftSpecification;
}>();
let controller = new AbortController();
let generation = 0;
const scopeKey = computed(() => runtimeResourceAddressKey(props.resourceScope));
const fingerprint = computed(() =>
  environmentDraftFingerprint(props.specification),
);
const dirty = computed(
  () =>
    !draft.value ||
    environmentDraftFingerprint(draft.value.specification) !==
      fingerprint.value,
);
const editable = computed(
  () =>
    props.canEdit &&
    !busy.value &&
    !pending.value &&
    (!draft.value || ["DRAFT", "VALID", "INVALID"].includes(draft.value.state)),
);
const freshAuthNeeded = computed(
  () =>
    problem.value?.code === "FRESH_AUTHENTICATION_REQUIRED" &&
    problem.value.status === 403,
);

function checkDraft(value: RuntimeEnvironmentDraft): void {
  assertRuntimeResourceAddressIdentity(
    props.resourceScope,
    value,
    props.environment.organizationRef,
  );
  if (
    value.environmentRef !== props.environment.ref ||
    value.expectedEnvironmentVersion !== props.environment.version
  )
    throw new Error("Assistant environment draft source version mismatch");
}
async function currentResult<T>(request: Promise<T>): Promise<T> {
  const current = generation,
    signal = controller.signal;
  const value = await request;
  if (current !== generation || signal.aborted)
    throw new DOMException("Stale assistant environment request", "AbortError");
  return value;
}
async function run(action: () => Promise<void>): Promise<void> {
  if (busy.value) return;
  const current = generation;
  busy.value = true;
  problem.value = undefined;
  try {
    await action();
  } catch (error) {
    if (current === generation && !controller.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) busy.value = false;
  }
}
async function resume(): Promise<void> {
  const ref = props.initialDraftRef;
  if (!ref) return;
  await run(async () => {
    const value = await currentResult(
      readEnvironmentDraft(props.resourceScope, ref, controller.signal),
    );
    checkDraft(value);
    if (
      props.expectedDraftVersion !== undefined &&
      value.version !== props.expectedDraftVersion
    )
      throw new Error(
        "Assistant environment reauthentication draft version mismatch",
      );
    draft.value = value;
    pending.value = readPublicationAttempt(
      "RUNTIME_ENVIRONMENT",
      value.ref,
      window.sessionStorage,
    );
    emit("draftLoaded", value);
  });
}
async function save(): Promise<void> {
  if (!editable.value || !dirty.value) return;
  if (
    saveAttempt.value &&
    saveAttempt.value.fingerprint !== fingerprint.value
  ) {
    problem.value = asProblem(
      new Error(
        "Unresolved environment draft save must be reconciled before editing",
      ),
    );
    return;
  }
  await run(async () => {
    const attempt = saveAttempt.value ?? {
      key: idempotencyKey(),
      fingerprint: fingerprint.value,
      draft: draft.value,
      specification: structuredClone(props.specification),
    };
    saveAttempt.value = attempt;
    const result = attempt.draft
      ? await currentResult(
          saveEnvironmentDraft(
            attempt.draft,
            attempt.specification,
            controller.signal,
            attempt.key,
          ),
        )
      : await currentResult(
          createEnvironmentDraft(
            props.resourceScope,
            attempt.specification,
            controller.signal,
            props.environment,
            attempt.key,
          ),
        );
    checkDraft(result);
    draft.value = result;
    plan.value = undefined;
    saveAttempt.value = undefined;
    emit("draftSaved", result);
  });
}
async function validate(): Promise<void> {
  const current = draft.value;
  if (!editable.value || !current || dirty.value || saveAttempt.value) return;
  await run(async () => {
    const value = await currentResult(
      transitionEnvironmentDraft("validate", current, controller.signal),
    );
    checkDraft(value);
    draft.value = value;
  });
}
async function preview(): Promise<void> {
  const current = draft.value;
  if (
    !editable.value ||
    !current ||
    dirty.value ||
    !props.valid ||
    current.state !== "VALID"
  )
    return;
  await run(async () => {
    plan.value = await currentResult(
      prepareEnvironmentPublication(current, controller.signal),
    );
  });
}
async function publish(selected: string[]): Promise<void> {
  const current = draft.value,
    currentPlan = plan.value;
  if (
    !editable.value ||
    !current ||
    !currentPlan ||
    dirty.value ||
    !props.valid
  )
    return;
  await run(async () => {
    const fresh = await currentResult(
      readEnvironmentDraft(props.resourceScope, current.ref, controller.signal),
    );
    checkDraft(fresh);
    if (fresh.version !== current.version || fresh.state !== "VALID")
      throw new Error(
        "Assistant environment draft changed before confirmation",
      );
    const attempt: PublicationAttempt = {
      kind: "RUNTIME_ENVIRONMENT",
      ownerRef: current.ref,
      planRef: currentPlan.ref,
      version: current.version,
      selectedItemRefs: [...selected],
      key: idempotencyKey(),
    };
    rememberPublicationAttempt(attempt, window.sessionStorage);
    pending.value = attempt;
    try {
      const result = await currentResult(
        publishEnvironmentDraft(
          current,
          currentPlan,
          selected,
          controller.signal,
          attempt.key,
        ),
      );
      forgetPublicationAttempt(
        "RUNTIME_ENVIRONMENT",
        current.ref,
        window.sessionStorage,
      );
      pending.value = undefined;
      draft.value = result.draft;
      plan.value = undefined;
      emit("published", result.environment);
    } catch (error) {
      const value = asProblem(error);
      if (
        value.status === 403 &&
        value.code === "FRESH_AUTHENTICATION_REQUIRED"
      ) {
        forgetPublicationAttempt(
          "RUNTIME_ENVIRONMENT",
          current.ref,
          window.sessionStorage,
        );
        pending.value = undefined;
        plan.value = undefined;
      }
      throw error;
    }
  });
}
async function reconcile(): Promise<void> {
  const attempt = pending.value,
    previous = draft.value;
  if (!attempt || !previous) return;
  await run(async () => {
    const report = await currentResult(
      restorePublicationImpact(attempt.planRef, controller.signal),
    );
    const current = await currentResult(
      readEnvironmentDraft(
        props.resourceScope,
        previous.ref,
        controller.signal,
      ),
    );
    assertRuntimeResourceAddressIdentity(
      props.resourceScope,
      current,
      props.environment.organizationRef,
    );
    if (
      report.plan.kind !== "RUNTIME_ENVIRONMENT" ||
      report.plan.draftRef !== current.ref ||
      report.plan.draftVersion !== attempt.version ||
      (plan.value &&
        publicationPlanIdentity(report.plan) !==
          publicationPlanIdentity(plan.value))
    )
      throw new Error("Assistant environment publication recovery mismatch");
    if (report.plan.state === "APPLIED" && current.state === "PUBLISHED") {
      forgetPublicationAttempt(
        "RUNTIME_ENVIRONMENT",
        current.ref,
        window.sessionStorage,
      );
      pending.value = undefined;
      draft.value = current;
      plan.value = undefined;
      emit("draftSaved", current);
    } else if (
      report.plan.state === "EXPIRED" &&
      current.state === "VALID" &&
      current.version === attempt.version
    ) {
      forgetPublicationAttempt(
        "RUNTIME_ENVIRONMENT",
        current.ref,
        window.sessionStorage,
      );
      pending.value = undefined;
      plan.value = undefined;
    } else
      throw new Error(
        "Assistant environment publication outcome is not confirmed",
      );
  });
}
watch(busy, (value) => emit("busy", value));
watch(
  [scopeKey, () => props.environment.ref, () => props.initialDraftRef],
  () => {
    generation += 1;
    controller.abort();
    controller = new AbortController();
    draft.value = undefined;
    plan.value = undefined;
    pending.value = undefined;
    saveAttempt.value = undefined;
    busy.value = false;
    problem.value = undefined;
    void resume();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  generation += 1;
  controller.abort();
});
</script>

<template>
  <section class="environment-draft-actions">
    <p class="secondary-text">
      {{ $t("assistant.resources.publicationHelp") }}
    </p>
    <StatusBadge v-if="draft" :state="draft.state" />
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <ul v-if="draft?.diagnostics.length" class="field-error">
      <li v-for="message in draft.diagnostics" :key="message">{{ message }}</li>
    </ul>
    <div class="environment-draft-actions__buttons">
      <button class="button" :disabled="!editable || !dirty" @click="save">
        {{ $t("managed.saveDraft") }}
      </button>
      <button
        class="button"
        :disabled="!editable || !draft || dirty || Boolean(saveAttempt)"
        @click="validate"
      >
        {{ $t("managed.validate") }}
      </button>
      <button
        class="button button--primary"
        :disabled="!editable || draft?.state !== 'VALID' || dirty || !valid"
        @click="preview"
      >
        {{ $t("managed.impact") }}
      </button>
      <button v-if="pending" class="button" :disabled="busy" @click="reconcile">
        {{ $t("runtime.reload") }}
      </button>
      <button
        v-if="freshAuthNeeded && draft"
        class="button"
        :disabled="busy"
        @click="emit('reauthenticate', draft)"
      >
        {{ $t("runtimeSecrets.reauthenticate") }}
      </button>
    </div>
    <ModalDialog
      v-if="plan && !pending"
      :title="$t('managed.impact')"
      :busy="busy"
      size="xl"
      @close="plan = undefined"
    >
      <PublicationImpactSelection
        :plan="plan"
        :busy="busy"
        @publish="publish"
      />
    </ModalDialog>
  </section>
</template>

<style scoped>
.environment-draft-actions {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.environment-draft-actions__buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.environment-draft-actions__buttons .button {
  height: 32px;
  min-height: 32px;
}
</style>
