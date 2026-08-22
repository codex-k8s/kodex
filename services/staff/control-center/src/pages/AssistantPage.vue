<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const { t } = useI18n();
const selectedRef = ref<string>();
const message = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
const log = ref<HTMLElement>();
const projectRef = computed(() =>
  typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : undefined,
);
const conversationList = computed(() =>
  Object.values(platform.conversations).sort((a, b) =>
    b.updatedAt.localeCompare(a.updatedAt),
  ),
);
const conversation = computed(() =>
  selectedRef.value
    ? platform.conversations[selectedRef.value]
    : conversationList.value[0],
);

async function ensureConversation(): Promise<string> {
  if (conversation.value) return conversation.value.ref;
  const created = await platform.newConversation(
    t("assistant.newConversation"),
    projectRef.value,
  );
  selectedRef.value = created.ref;
  return created.ref;
}

async function send(): Promise<void> {
  const content = message.value.trim();
  if (!content) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const ref = await ensureConversation();
    const updated = await platform.sendAssistantTurn(ref, content);
    selectedRef.value = updated.ref;
    message.value = "";
    await nextTick();
    log.value?.scrollTo({ top: log.value.scrollHeight, behavior: "smooth" });
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function apply(planRef: string, version: number): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.applyPlan(planRef, version);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => void platform.loadAssistant());
</script>

<template>
  <PageFrame
    :title="$t('assistant.title')"
    :subtitle="$t('assistant.subtitle')"
  >
    <template #actions
      ><StatusBadge
        v-if="platform.assistant"
        :state="platform.assistant.runtimeState"
    /></template>
    <AsyncState
      :loading="platform.loading.assistant"
      :problem="platform.problems.assistant"
      @retry="platform.loadAssistant()"
    >
      <div class="assistant-workspace">
        <aside class="conversation-list">
          <button
            class="button button--primary"
            type="button"
            @click="selectedRef = undefined"
          >
            {{ $t("assistant.newConversation") }}
          </button>
          <button
            v-for="item in conversationList"
            :key="item.ref"
            type="button"
            :class="{ selected: item.ref === conversation?.ref }"
            @click="selectedRef = item.ref"
          >
            <strong>{{ item.title }}</strong
            ><small>{{ new Date(item.updatedAt).toLocaleString() }}</small>
          </button>
        </aside>
        <section class="assistant-chat">
          <header>
            <div>
              <strong>{{ $t("assistant.ready") }}</strong>
              <p>{{ $t("assistant.system") }}</p>
            </div>
            <span class="audit-note">{{ $t("assistant.audit") }}</span>
          </header>
          <div ref="log" class="chat-log" role="log" aria-live="polite">
            <div v-if="!conversation?.turns.length" class="assistant-empty">
              {{ $t("assistant.empty") }}
            </div>
            <article
              v-for="turn in conversation?.turns ?? []"
              :key="turn.ref"
              class="chat-turn"
              :class="`chat-turn--${turn.role.toLowerCase()}`"
            >
              <div class="chat-turn__meta">
                <span>{{
                  turn.role === "USER"
                    ? $t("common.input")
                    : $t("app.assistantShort")
                }}</span
                ><StatusBadge :state="turn.state" />
              </div>
              <p>{{ turn.content }}</p>
              <section v-if="turn.plan" class="assistant-plan">
                <h3>{{ $t("assistant.plan") }}</h3>
                <ol>
                  <li
                    v-for="operation in turn.plan.operations"
                    :key="operation.ref"
                  >
                    <span>{{ operation.title }}</span
                    ><small>{{ operation.summary }}</small
                    ><StatusBadge
                      :state="operation.permitted ? 'READY' : 'UNAVAILABLE'"
                    />
                  </li>
                </ol>
                <button
                  v-if="turn.plan.nextActions.includes('APPLY_PLAN')"
                  class="button button--primary"
                  type="button"
                  :disabled="busy"
                  @click="apply(turn.plan!.ref, turn.plan!.version)"
                >
                  {{ $t("assistant.applyPlan") }}
                </button>
              </section>
            </article>
          </div>
          <ProblemNotice v-if="problem" :problem="problem" compact />
          <form class="composer" @submit.prevent="send">
            <label class="sr-only" for="assistant-message">{{
              $t("assistant.message")
            }}</label
            ><textarea
              id="assistant-message"
              v-model="message"
              :placeholder="$t('assistant.message')"
              maxlength="8000"
              @keydown.ctrl.enter="send"
            /><button
              class="button button--primary"
              type="submit"
              :disabled="busy || !message.trim()"
            >
              {{ $t("assistant.send") }}
            </button>
          </form>
        </section>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.assistant-workspace {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  min-height: calc(100vh - 190px);
  border: 1px solid var(--border);
  border-radius: 11px;
  overflow: hidden;
  background: var(--surface);
}
.conversation-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 14px;
  border-right: 1px solid var(--border);
  background: var(--panel);
}
.conversation-list > button:not(.button) {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  text-align: left;
  cursor: pointer;
}
.conversation-list > button.selected {
  background: var(--accent-soft);
}
.conversation-list small {
  color: var(--muted);
}
.assistant-chat {
  display: grid;
  grid-template-rows: auto 1fr auto auto;
  min-width: 0;
}
.assistant-chat > header {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  padding: 15px 18px;
  border-bottom: 1px solid var(--border);
}
.assistant-chat header p {
  margin: 2px 0 0;
  color: var(--muted);
}
.audit-note {
  max-width: 340px;
  color: var(--muted);
  font-size: 0.82rem;
}
.chat-log {
  overflow-y: auto;
  padding: 20px;
}
.assistant-empty {
  max-width: 620px;
  margin: 80px auto;
  color: var(--muted);
  text-align: center;
}
.chat-turn {
  max-width: 760px;
  margin: 0 0 16px;
  padding: 13px 15px;
  border: 1px solid var(--border);
  border-radius: 10px;
}
.chat-turn--user {
  margin-left: auto;
  background: var(--accent-soft);
}
.chat-turn__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
  font-size: 0.82rem;
  font-weight: 600;
}
.chat-turn p {
  white-space: pre-wrap;
}
.assistant-plan {
  padding: 12px;
  border: 1px solid #cbdff3;
  border-radius: 8px;
  background: var(--surface);
}
.assistant-plan ol {
  display: grid;
  gap: 8px;
  padding-left: 22px;
}
.assistant-plan li {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 2px 12px;
}
.assistant-plan small {
  color: var(--muted);
}
.composer {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 10px;
  padding: 14px;
  border-top: 1px solid var(--border);
}
.composer textarea {
  min-height: 68px;
}
.composer button {
  align-self: end;
}
@media (max-width: 800px) {
  .assistant-workspace {
    grid-template-columns: 1fr;
  }
  .conversation-list {
    display: none;
  }
  .assistant-chat > header {
    flex-direction: column;
  }
  .composer {
    grid-template-columns: 1fr;
  }
  .composer button {
    width: 100%;
  }
  .chat-log {
    padding: 12px;
  }
}
</style>
