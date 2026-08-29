<script setup lang="ts">
import { Sparkles, Trash2, Upload } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import AgentAvatar from "@/features/agents/detail/AgentAvatar.vue";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import type {
  AgentBackendFeatureAvailability,
  AgentProfileDraft,
} from "@/features/agents/detail/model";

const props = defineProps<{
  modelValue: AgentProfileDraft;
  roleName: string;
  canEdit: boolean;
  busy: boolean;
  dirty: boolean;
  avatarUrl?: string;
  avatarAsset: AgentBackendFeatureAvailability;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: AgentProfileDraft];
  "upload-avatar": [file: File];
  "remove-avatar": [];
  save: [];
}>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const fileInput = ref<HTMLInputElement>();
const avatarAvailable = computed(() => props.avatarAsset.state === "AVAILABLE");
const avatarUnavailableReason = computed(() =>
  props.avatarAsset.state === "UNAVAILABLE"
    ? props.avatarAsset.reason
    : undefined,
);

function updateField(key: keyof AgentProfileDraft, event: Event): void {
  const target = event.currentTarget;
  if (
    !(
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement
    )
  )
    return;
  emit("update:modelValue", { ...props.modelValue, [key]: target.value });
}

function chooseAvatar(): void {
  if (avatarAvailable.value) fileInput.value?.click();
}

function uploadAvatar(event: Event): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLInputElement)) return;
  const file = target.files?.[0];
  target.value = "";
  if (file && avatarAvailable.value) emit("upload-avatar", file);
}
</script>

<template>
  <article class="profile-panel panel">
    <div class="profile-panel__identity">
      <AgentAvatar
        :name="modelValue.name"
        :url="avatarUrl"
        :label="copy.avatar.preview"
      />
      <div>
        <h2>{{ modelValue.name }}</h2>
        <p>{{ roleName }}</p>
        <span class="profile-panel__avatar-state">
          {{ avatarUrl ? $t("agents.avatar") : copy.avatar.fallback }}
        </span>
      </div>
    </div>

    <form class="profile-panel__form" @submit.prevent="emit('save')">
      <label class="field">
        <span>{{ $t("common.name") }}</span>
        <input
          :value="modelValue.name"
          required
          maxlength="120"
          :disabled="!canEdit || busy"
          @input="updateField('name', $event)"
        />
      </label>
      <label class="field">
        <span>{{ $t("common.purpose") }}</span>
        <input
          :value="modelValue.purpose"
          required
          maxlength="1000"
          :disabled="!canEdit || busy"
          @input="updateField('purpose', $event)"
        />
      </label>
      <label class="field field--wide">
        <span>{{ $t("agents.role") }}</span>
        <textarea
          :value="modelValue.roleDescription"
          required
          maxlength="1000"
          :disabled="!canEdit || busy"
          @input="updateField('roleDescription', $event)"
        />
      </label>
      <section class="profile-panel__avatar-editor field--wide">
        <div>
          <strong>{{ $t("agents.avatar") }}</strong>
          <p>{{ copy.avatar.help }}</p>
        </div>
        <input
          ref="fileInput"
          class="sr-only"
          type="file"
          accept="image/png,image/jpeg,image/webp"
          :disabled="!canEdit || busy || !avatarAvailable"
          @change="uploadAvatar"
        />
        <div class="profile-panel__avatar-actions">
          <button
            class="button"
            type="button"
            disabled
            :title="$t('common.unavailable')"
          >
            <Sparkles :size="16" aria-hidden="true" />{{ copy.avatar.generate }}
          </button>
          <button
            class="button"
            type="button"
            :disabled="!canEdit || busy || !avatarAvailable"
            :title="avatarUnavailableReason"
            @click="chooseAvatar"
          >
            <Upload :size="16" aria-hidden="true" />{{ copy.avatar.upload }}
          </button>
          <button
            class="button button--danger"
            type="button"
            :disabled="!canEdit || busy || !avatarUrl || !avatarAvailable"
            :title="avatarUnavailableReason"
            @click="emit('remove-avatar')"
          >
            <Trash2 :size="16" aria-hidden="true" />{{ copy.avatar.remove }}
          </button>
        </div>
        <code v-if="avatarAsset.state === 'UNAVAILABLE'">
          {{ avatarAsset.code }}: {{ $t("states.UNAVAILABLE") }} ·
          {{ avatarAsset.reason }}
        </code>
      </section>
      <div v-if="canEdit" class="profile-panel__actions field--wide">
        <span v-if="dirty" class="profile-panel__dirty">
          {{ $t("states.DRAFT") }}
        </span>
        <button
          class="button button--primary"
          type="submit"
          :disabled="
            busy ||
            !dirty ||
            !modelValue.name.trim() ||
            !modelValue.purpose.trim() ||
            !modelValue.roleDescription.trim()
          "
        >
          {{ copy.profile.save }}
        </button>
      </div>
    </form>
  </article>
</template>

<style scoped>
.profile-panel {
  display: grid;
  gap: 20px;
}
.profile-panel__identity {
  display: flex;
  align-items: center;
  gap: 16px;
  min-width: 0;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.profile-panel__identity > div {
  min-width: 0;
}
.profile-panel__identity h2,
.profile-panel__identity p {
  margin: 0;
  overflow-wrap: anywhere;
}
.profile-panel__identity h2 {
  font-size: 1.15rem;
}
.profile-panel__identity p {
  margin-top: 4px;
  color: var(--muted);
}
.profile-panel__avatar-state {
  display: inline-block;
  margin-top: 7px;
  color: var(--subtle);
  font-size: 0.78rem;
}
.profile-panel__form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.profile-panel__avatar-editor {
  display: grid;
  gap: 10px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.profile-panel__avatar-editor p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 0.8rem;
}
.profile-panel__avatar-actions,
.profile-panel__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.profile-panel__avatar-editor > code {
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.76rem;
  overflow-wrap: anywhere;
}
.profile-panel__actions {
  justify-content: flex-end;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.profile-panel__dirty {
  margin-right: auto;
  color: var(--warning);
  font-size: 0.8rem;
}
.field--wide {
  grid-column: 1 / -1;
}
@media (max-width: 640px) {
  .profile-panel__form {
    grid-template-columns: 1fr;
  }
  .field--wide {
    grid-column: auto;
  }
  .profile-panel__identity {
    align-items: flex-start;
  }
}
</style>
