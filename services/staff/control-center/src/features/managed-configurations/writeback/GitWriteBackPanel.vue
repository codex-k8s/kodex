<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  ref,
  shallowRef,
  shallowReactive,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import CodeEditor from "@/shared/ui/CodeEditor.vue";
import CodeDiff from "@/shared/ui/CodeDiff.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { WriteBackController } from "./controller";
import {
  actionReason,
  contentBytes,
  maximumContentBytes,
  matchesPreparation,
  pollsNeeded,
  preparationReason,
  safePullRequestUrl,
  type Action,
} from "./model";
import { writeBackMessages } from "./messages";

const props = defineProps<{
  configuration: ManagedConfiguration;
  disabled?: boolean;
}>();
const emit = defineEmits<{ changed: []; busy: [boolean] }>();
const { t, locale } = useI18n({
  useScope: "local",
  messages: writeBackMessages,
});
const state = shallowRef<WriteBackController>();
const editing = ref(false);
const approved = ref(false);
const adopted = ref(false);
const now = ref(Date.now());
let timer: ReturnType<typeof setTimeout> | undefined;
let stopOwner: (() => void) | undefined;
const view = computed(() => state.value?.view);
const proposal = computed(() => view.value?.proposal);
const blocked = computed(
  () =>
    props.disabled ||
    (state.value?.working ?? false) ||
    !state.value ||
    state.value.signal.aborted,
);
const reason = computed(() => preparationReason(props.configuration));
const pending = computed(() => state.value?.pending);
const size = computed(() => contentBytes(state.value?.content ?? ""));
const actions: Action[] = ["APPROVE", "REJECT", "CANCEL"];
function time(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
function stop(): void {
  clearTimeout(timer);
  stopOwner?.();
  state.value?.close();
}
function schedule(): void {
  clearTimeout(timer);
  const current = state.value;
  if (!current || current.signal.aborted) return;
  timer = setTimeout(() => {
    void tick(current);
  }, 3000);
}
async function tick(current: WriteBackController): Promise<void> {
  now.value = Date.now();
  if (!props.disabled && !current.paused && current === state.value)
    await current.poll();
  if (current === state.value) schedule();
}
watch(
  () => props.configuration.ref,
  () => {
    stop();
    const owner = ownerRequestSignal();
    const current = shallowReactive(
      new WriteBackController(props.configuration, sessionStorage, owner),
    );
    state.value = current;
    editing.value = current.pending?.action === "PREPARE";
    approved.value = false;
    adopted.value = false;
    const revoked = () => {
      current.revoke();
      editing.value = false;
      approved.value = false;
      emit("busy", false);
      clearTimeout(timer);
    };
    owner.addEventListener("abort", revoked, { once: true });
    stopOwner = () => owner.removeEventListener("abort", revoked);
    if (
      ["ROLE_IMAGE", "INTEGRATION_DEFINITION"].includes(
        props.configuration.kind,
      ) &&
      !props.disabled
    ) {
      if (current.pending?.proposalRef) void current.recover();
      else void current.history();
    }
    schedule();
  },
  { immediate: true },
);
watch(
  () => props.configuration,
  (configuration) => state.value?.update(configuration),
);
watch(
  () => props.disabled,
  (disabled) => {
    if (
      !disabled &&
      state.value &&
      !state.value.loaded &&
      !state.value.problem &&
      !state.value.working &&
      !state.value.signal.aborted
    )
      void state.value.history();
  },
);
watch(
  () =>
    `${proposal.value?.ref ?? ""}/${String(proposal.value?.version)}/${proposal.value?.approvalDigest ?? ""}`,
  () => {
    approved.value = false;
    adopted.value = false;
  },
);
watch(
  () => state.value?.working ?? false,
  (busy) => emit("busy", busy),
  { immediate: true },
);
onBeforeUnmount(() => {
  stop();
  emit("busy", false);
});
function edit(): void {
  if (!state.value || blocked.value || pending.value || reason.value) return;
  state.value.content = props.configuration.currentRevision?.content ?? "";
  editing.value = true;
}
async function prepare(): Promise<void> {
  const current = state.value;
  if (!current || blocked.value) return;
  await current.prepare();
  if (current.signal.aborted || current !== state.value) return;
  if (!current.pending && current.view) {
    editing.value = false;
    emit("changed");
    await current.history();
  }
}
async function decide(action: Action): Promise<void> {
  const current = state.value;
  if (
    !current ||
    blocked.value ||
    (action === "APPROVE" && !approved.value && !pending.value)
  )
    return;
  await current.decide(action);
  if (!current.signal.aborted && current === state.value && !current.pending) {
    emit("changed");
    await current.history();
  }
}
async function refresh(): Promise<void> {
  const current = state.value;
  if (!current || blocked.value) return;
  current.polls = 0;
  current.paused = false;
  if (current.view || current.pending?.proposalRef) await current.recover();
  else await current.history();
}
function discardRejected(): void {
  const current = state.value;
  if (!current || blocked.value || !current.rejected) return;
  current.discardRejected();
  if (!current.pending) emit("changed");
}
async function adopt(): Promise<void> {
  const current = state.value;
  if (!current || blocked.value || !adopted.value) return;
  await current.adoptPrepared();
  if (current === state.value && !current.signal.aborted && !current.pending) {
    editing.value = false;
    adopted.value = false;
    approved.value = false;
  }
}
</script>

<template>
  <section
    class="writeback"
    :aria-label="t('wb.title')"
    :aria-busy="state?.working"
  >
    <header>
      <h3>{{ t("wb.title") }}</h3>
      <button type="button" :disabled="blocked" @click="refresh">
        {{ t("wb.refresh") }}
      </button>
    </header>
    <p>{{ t("wb.intro") }}</p>
    <p v-if="reason" class="muted">{{ t(`wb.disabled.${reason}`) }}</p>
    <p v-else class="muted">{{ t("wb.permission") }}</p>
    <ProblemNotice v-if="state?.problem" :problem="state.problem" />
    <p v-if="state?.working" role="status">{{ t("wb.preparing") }}</p>
    <div v-if="pending" class="notice" role="status">
      <p>{{ t(state?.rejected ? "wb.rejectedInput" : "wb.unknown") }}</p>
      <button
        v-if="state?.rejected"
        type="button"
        :disabled="blocked"
        @click="discardRejected"
      >
        {{ t("wb.discardRejected") }}
      </button>
      <p v-if="pending.action === 'PREPARE'">{{ t("wb.restore") }}</p>
      <button type="button" :disabled="blocked" @click="refresh">
        {{ t("wb.recover") }}
      </button>
      <button
        v-if="pending.action !== 'PREPARE'"
        type="button"
        :disabled="blocked"
        @click="decide(pending.action)"
      >
        {{ t("wb.retry") }}
      </button>
    </div>
    <button
      v-if="!editing"
      type="button"
      :disabled="blocked || !!reason || !!pending || !!state?.stale"
      @click="edit"
    >
      {{ t("wb.edit") }}
    </button>
    <div v-if="editing && state" class="document">
      <p>{{ t("wb.inputHint") }}</p>
      <CodeEditor
        v-model="state.content"
        :label="t('wb.edit')"
        :disabled="blocked"
        language="yaml"
      />
      <p :class="{ invalid: size > maximumContentBytes }">
        {{ t("wb.bytes", { count: size, max: maximumContentBytes }) }}
      </p>
      <button
        type="button"
        :disabled="
          blocked ||
          !size ||
          size > maximumContentBytes ||
          (!pending && !!reason)
        "
        @click="prepare"
      >
        {{ t(pending ? "wb.retryPrepare" : "wb.prepare") }}
      </button>
    </div>
    <section class="history" :aria-label="t('wb.history')">
      <header>
        <h4>{{ t("wb.history") }}</h4>
        <span>{{ t("wb.count", { count: state?.total ?? 0 }) }}</span
        ><button type="button" :disabled="blocked" @click="state?.history()">
          {{ t("wb.refresh") }}
        </button>
      </header>
      <p v-if="!state?.items.length && !state?.working">{{ t("wb.empty") }}</p>
      <ul>
        <li v-for="item in state?.items" :key="item.ref">
          <button
            type="button"
            :disabled="
              blocked ||
              (!!pending &&
                pending.action !== 'PREPARE' &&
                pending.proposalRef !== item.ref)
            "
            :aria-label="`${t('wb.inspect')}: ${item.ref}`"
            @click="state?.select(item)"
          >
            <span>{{ t(`wb.state.${item.state}`) }}</span
            ><time :datetime="item.createdAt">{{ time(item.createdAt) }}</time
            ><code>{{ item.ref }}</code>
          </button>
        </li>
      </ul>
      <button
        v-if="state?.cursor"
        type="button"
        :disabled="blocked"
        @click="state.history(true)"
      >
        {{ t("wb.more") }}
      </button>
    </section>
    <section v-if="view && proposal" class="plan" :aria-label="t('wb.inspect')">
      <h4>{{ t(`wb.state.${proposal.state}`) }}</h4>
      <p v-if="proposal.failureCode" role="status">
        {{ t(`wb.failure.${proposal.failureCode}`) }}
      </p>
      <p v-if="proposal.state === 'SUCCEEDED'">{{ t("wb.success") }}</p>
      <p v-if="proposal.state === 'UNKNOWN_OUTCOME'">
        {{ t("wb.unknownEffect") }}
      </p>
      <dl>
        <dt>{{ t("wb.repository") }}</dt>
        <dd>{{ proposal.repositoryRef }}</dd>
        <dt>{{ t("wb.branch") }}</dt>
        <dd>{{ proposal.sourceRefName }}</dd>
        <dt>{{ t("wb.proposalBranch") }}</dt>
        <dd>{{ proposal.proposalBranch }}</dd>
        <dt>{{ t("wb.path") }}</dt>
        <dd>{{ proposal.path }}</dd>
        <dt>{{ t("wb.commit") }}</dt>
        <dd>
          <code>{{ proposal.baseCommitSha }}</code>
        </dd>
        <dt>{{ t("wb.digest") }}</dt>
        <dd>
          <code>{{ proposal.approvalDigest }}</code>
        </dd>
        <dt>{{ t("wb.expires") }}</dt>
        <dd>{{ time(proposal.expiresAt) }}</dd>
        <template v-if="proposal.branchConfirmedAt"
          ><dt>{{ t("wb.receiptBranch") }}</dt>
          <dd>{{ time(proposal.branchConfirmedAt) }}</dd></template
        >
        <template v-if="proposal.pullRequestConfirmedAt"
          ><dt>{{ t("wb.receiptPR") }}</dt>
          <dd>{{ time(proposal.pullRequestConfirmedAt) }}</dd></template
        >
      </dl>
      <a
        v-if="safePullRequestUrl(proposal.pullRequestUrl)"
        :href="safePullRequestUrl(proposal.pullRequestUrl)"
        target="_blank"
        rel="noopener noreferrer"
        >{{ t("wb.pr") }}</a
      >
      <CodeDiff
        :original="view.baseContent"
        :modified="view.proposedContent"
        :label="t('wb.diff')"
      />
      <div v-if="pending?.action === 'PREPARE'" class="notice">
        <p>{{ t("wb.adoptExplanation") }}</p>
        <label
          ><input
            v-model="adopted"
            type="checkbox"
            :disabled="blocked || !matchesPreparation(pending, proposal)"
          />{{ t("wb.adoptConfirm") }}</label
        >
        <button
          type="button"
          :disabled="
            blocked || !adopted || !matchesPreparation(pending, proposal)
          "
          @click="adopt"
        >
          {{ t("wb.adopt") }}
        </button>
        <p v-if="!matchesPreparation(pending, proposal)">
          {{ t("wb.adoptMismatch") }}
        </p>
      </div>
      <details>
        <summary>{{ t("wb.base") }}</summary>
        <CodeEditor
          :model-value="view.baseContent"
          readonly
          language="yaml"
          :label="t('wb.base')"
        />
      </details>
      <details>
        <summary>{{ t("wb.proposed") }}</summary>
        <CodeEditor
          :model-value="view.proposedContent"
          readonly
          language="yaml"
          :label="t('wb.proposed')"
        />
      </details>
      <p v-if="state?.stale" role="status">{{ t("wb.stale") }}</p>
      <label v-if="!actionReason(proposal, 'APPROVE', now)"
        ><input
          v-model="approved"
          type="checkbox"
          :disabled="blocked || !!pending || !!state?.stale"
        />{{ t("wb.confirmed") }}</label
      >
      <div class="actions">
        <div v-for="action in actions" :key="action">
          <button
            type="button"
            :disabled="
              blocked ||
              !!pending ||
              !!state?.stale ||
              !!actionReason(proposal, action, now) ||
              (action === 'APPROVE' && !approved)
            "
            @click="decide(action)"
          >
            {{ t(`wb.${action}`) }}
          </button>
          <small v-if="actionReason(proposal, action, now)">{{
            t(`wb.reason.${actionReason(proposal, action, now)}`)
          }}</small>
        </div>
      </div>
      <p v-if="state?.paused && pollsNeeded(proposal)" role="status">
        {{ t("wb.paused") }}
        <button type="button" :disabled="blocked" @click="refresh">
          {{ t("wb.resume") }}
        </button>
      </p>
    </section>
  </section>
</template>

<style scoped>
.writeback {
  display: grid;
  gap: 12px;
  min-width: 0;
  max-width: 100%;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
header,
.actions {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
  justify-content: space-between;
}
h3,
h4,
p {
  margin: 0;
}
.muted,
small {
  color: var(--text-muted);
}
.notice {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  display: grid;
  gap: 8px;
}
.document,
.plan,
.history,
.actions > div {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.history ul {
  list-style: none;
  padding: 0;
  margin: 0;
  max-height: 320px;
  overflow: auto;
}
.history li button {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  text-align: start;
  padding: 10px;
}
code,
dd,
a {
  overflow-wrap: anywhere;
  min-width: 0;
}
dl {
  display: grid;
  grid-template-columns: minmax(100px, 1fr) minmax(0, 3fr);
  gap: 6px 12px;
  margin: 0;
}
dd {
  margin: 0;
}
label {
  display: flex;
  align-items: start;
  gap: 8px;
}
.invalid {
  color: var(--danger);
}
@media (max-width: 600px) {
  .writeback {
    padding: 10px;
  }
  dl {
    grid-template-columns: 1fr;
  }
  dd {
    margin-bottom: 8px;
  }
  .actions {
    align-items: stretch;
  }
}
</style>
