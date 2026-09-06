<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { Trash2, Upload } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import AgentAvatar from "@/features/agents/detail/AgentAvatar.vue";
import { supportedAvatarFile } from "@/features/agents/detail/avatar";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import type {
  AgentBackendFeatureAvailability,
  AgentProfileDraft,
} from "@/features/agents/detail/model";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

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
const avatarProblem = ref("");
const removeConfirmationOpen = ref(false);
const avatarDragging = ref(false);
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
  if (!props.canEdit || props.busy || !avatarAvailable.value) return;
  avatarProblem.value = "";
  fileInput.value?.click();
}

function selectAvatarFile(file: File | undefined): void {
  if (!file || !avatarAvailable.value) return;
  if (!supportedAvatarFile(file)) {
    avatarProblem.value = copy.value.avatar.typeError;
    return;
  }
  avatarProblem.value = "";
  emit("upload-avatar", file);
}

function uploadAvatar(event: Event): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLInputElement)) return;
  const file = target.files?.[0];
  target.value = "";
  selectAvatarFile(file);
}

function dropAvatar(event: DragEvent): void {
  avatarDragging.value = false;
  if (!props.canEdit || props.busy || !avatarAvailable.value) return;
  selectAvatarFile(event.dataTransfer?.files[0]);
}

function leaveAvatar(event: DragEvent): void {
  const next = event.relatedTarget;
  if (
    next instanceof Node &&
    event.currentTarget instanceof HTMLElement &&
    event.currentTarget.contains(next)
  )
    return;
  avatarDragging.value = false;
}

function requestAvatarRemoval(): void {
  if (
    !props.canEdit ||
    props.busy ||
    !props.avatarUrl ||
    !avatarAvailable.value
  )
    return;
  removeConfirmationOpen.value = true;
}

function confirmAvatarRemoval(): void {
  removeConfirmationOpen.value = false;
  emit("remove-avatar");
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
        <VoiceTextarea
          :value="modelValue.roleDescription"
          required
          maxlength="1000"
          :disabled="!canEdit || busy"
          @input="updateField('roleDescription', $event)"
        />
      </label>
      <section
        class="profile-panel__avatar-editor field--wide"
        :class="{
          'profile-panel__avatar-editor--dragging': avatarDragging,
        }"
        @dragenter.prevent="avatarDragging = true"
        @dragover.prevent
        @dragleave="leaveAvatar"
        @drop.prevent="dropAvatar"
      >
        <div>
          <strong>{{ $t("agents.avatar") }}</strong>
          <p>{{ copy.avatar.help }}</p>
          <p>{{ copy.avatar.dropHelp }}</p>
        </div>
        <input
          ref="fileInput"
          class="sr-only"
          type="file"
          accept="image/png,image/jpeg,image/webp"
          :aria-label="copy.avatar.upload"
          :disabled="!canEdit || busy || !avatarAvailable"
          @change="uploadAvatar"
        />
        <div class="profile-panel__avatar-actions">
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
            @click="requestAvatarRemoval"
          >
            <Trash2 :size="16" aria-hidden="true" />{{ copy.avatar.remove }}
          </button>
        </div>
        <p
          v-if="avatarProblem"
          class="profile-panel__avatar-problem"
          role="alert"
        >
          {{ avatarProblem }}
        </p>
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

    <ModalDialog
      v-if="removeConfirmationOpen"
      :title="copy.avatar.removeTitle"
      :busy="busy"
      size="sm"
      @close="removeConfirmationOpen = false"
    >
      <p class="profile-panel__remove-copy">
        {{ copy.avatar.removeConfirmation }}
      </p>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busy"
          @click="removeConfirmationOpen = false"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--danger"
          type="button"
          :disabled="busy"
          @click="confirmAvatarRemoval"
        >
          <Trash2 :size="16" aria-hidden="true" />{{ copy.avatar.remove }}
        </button>
      </template>
    </ModalDialog>
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
.profile-panel__avatar-editor--dragging {
  border-color: var(--accent);
  outline: 2px solid color-mix(in srgb, var(--accent) 20%, transparent);
  background: var(--accent-soft);
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
.profile-panel__avatar-problem,
.profile-panel__remove-copy {
  margin: 0;
}
.profile-panel__avatar-problem {
  color: var(--danger);
  font-size: 0.8rem;
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
