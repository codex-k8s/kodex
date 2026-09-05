<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import type {
  EmailMailboxSpecification,
  EmailMailboxEndpoint,
  EmailMailboxCredential,
  EmailMailboxCredentialReference,
  EmailMailboxOperation,
  EmailMailboxApprovalPolicy,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  value: EmailMailboxSpecification;
  credentials: EmailMailboxCredential[];
  disabled?: boolean;
}>();
const emit = defineEmits<{ change: [value: EmailMailboxSpecification] }>();
const endpointKinds = ["smtp", "imap", "pop"] as const;
const identityFields = [
  "sender",
  "replyTo",
  "helloName",
  "folder",
  "archiveFolder",
  "draftsFolder",
] as const;
const listFields = ["recipients", "allowedFolders"] as const;
const limitFields = [
  "attachmentBytes",
  "maxAttachments",
  "maxRecipients",
  "messageBytes",
  "pageSize",
  "scanMessages",
  "timeoutSeconds",
] as const;
const credentialFields = {
  ca: "CA_CERTIFICATE",
  username: "USERNAME",
  secret: "AUTH_SECRET",
} as const;
const operations: EmailMailboxOperation[] = [
  "HEALTH",
  "MAILBOXES",
  "LIST",
  "SEARCH",
  "FETCH",
  "DOWNLOAD",
  "SEND",
  "REPLY",
  "REPLY_ALL",
  "FORWARD",
  "DELETE",
  "RECEIPT",
  "THREAD",
  "ATTACHMENTS",
  "MARK_READ",
  "MARK_UNREAD",
  "MOVE",
  "ARCHIVE",
  "DRAFT_CREATE",
  "DRAFT_UPDATE",
  "DRAFT_DELETE",
];
function set<K extends keyof EmailMailboxSpecification>(
  key: K,
  value: EmailMailboxSpecification[K],
): void {
  if (props.disabled) return;
  const next = { ...props.value };
  if (value === undefined) Reflect.deleteProperty(next, key);
  else next[key] = value;
  emit("change", next);
}
function text(event: Event): string {
  return (event.target as HTMLInputElement).value;
}
function strings(value: string): string[] {
  return value
    .split("\n")
    .map((value) => value.trim())
    .filter(Boolean);
}
function number(event: Event): number | undefined {
  const value = text(event);
  return value === "" ? undefined : Number(value);
}
function endpoint<K extends keyof EmailMailboxEndpoint>(
  kind: (typeof endpointKinds)[number],
  key: K,
  value: EmailMailboxEndpoint[K],
): void {
  const next = { ...props.value[kind] };
  if (value === undefined) Reflect.deleteProperty(next, key);
  else next[key] = value;
  set(kind, next);
}
function credentialKey(value?: EmailMailboxCredentialReference): string {
  return value ? JSON.stringify([value.name, value.generation]) : "";
}
function credentialOptions(kind: keyof typeof credentialFields) {
  return props.credentials.filter(
    (value) => value.kind === credentialFields[kind],
  );
}
function selectCredential(
  kind: (typeof endpointKinds)[number],
  field: keyof typeof credentialFields,
  event: Event,
): void {
  const selected = credentialOptions(field).find(
    (value) => credentialKey(value) === text(event),
  );
  endpoint(
    kind,
    field,
    selected
      ? { name: selected.name, generation: selected.generation }
      : undefined,
  );
}
function missingCredential(
  value: EmailMailboxCredentialReference | undefined,
  field: keyof typeof credentialFields,
): boolean {
  return (
    Boolean(value) &&
    !credentialOptions(field).some(
      (item) => credentialKey(item) === credentialKey(value),
    )
  );
}
function addPolicy(): void {
  if ((props.value.policies?.length ?? 0) < operations.length)
    set("policies", [...(props.value.policies ?? []), {}]);
}
function policy(
  index: number,
  key: "operation" | "policy" | "folders",
  event: Event,
): void {
  const policies = (props.value.policies ?? []).map((value) => ({ ...value }));
  const item = policies[index];
  if (!item) return;
  if (key === "operation")
    item.operation = (text(event) || undefined) as
      | EmailMailboxOperation
      | undefined;
  else if (key === "policy")
    item.policy = (text(event) || undefined) as
      | EmailMailboxApprovalPolicy
      | undefined;
  else item.folders = strings(text(event));
  set("policies", policies);
}
</script>

<template>
  <fieldset class="mailbox-fields" :disabled="disabled">
    <label class="checkbox"
      ><input
        type="checkbox"
        :checked="value.enabled"
        @change="set('enabled', ($event.target as HTMLInputElement).checked)"
      />{{ $t("mailbox.enabled") }}</label
    >
    <label class="field"
      ><span>{{ $t("mailbox.receiveProtocol") }}</span
      ><select
        :value="value.receiveProtocol ?? ''"
        @change="
          set(
            'receiveProtocol',
            (text($event) ||
              undefined) as EmailMailboxSpecification['receiveProtocol'],
          )
        "
      >
        <option value=""></option>
        <option value="IMAP">IMAP</option>
        <option value="POP3">POP3</option>
      </select></label
    >
    <p v-if="value.receiveProtocol === 'POP3'">{{ $t("mailbox.popHelp") }}</p>
    <div class="mailbox-fields__grid">
      <label v-for="field in identityFields" :key="field" class="field"
        ><span>{{ $t(`mailbox.fields.${field}`) }}</span
        ><input
          :value="value[field] ?? ''"
          autocomplete="off"
          @input="set(field, text($event))"
      /></label>
      <label v-for="field in listFields" :key="field" class="field"
        ><span>{{ $t(`mailbox.fields.${field}`) }}</span
        ><VoiceTextarea
          :model-value="value[field]?.join('\n') ?? ''"
          :disabled="disabled"
          rows="3"
          autocomplete="off"
          spellcheck="false"
          @update:model-value="set(field, strings($event))"
        /><small>{{ $t("mailbox.linesHelp") }}</small></label
      >
    </div>
    <section
      v-for="kind in endpointKinds.filter(
        (kind) =>
          kind === 'smtp' ||
          (kind === 'imap'
            ? value.receiveProtocol !== 'POP3'
            : value.receiveProtocol === 'POP3'),
      )"
      :key="kind"
      :aria-label="kind.toUpperCase()"
      class="mailbox-fields__endpoint"
    >
      <h4>{{ kind.toUpperCase() }}</h4>
      <div class="mailbox-fields__grid">
        <label class="field"
          ><span>{{ $t("mailbox.host") }}</span
          ><input
            :value="value[kind]?.host ?? ''"
            autocomplete="off"
            @input="endpoint(kind, 'host', text($event))"
        /></label>
        <label class="field"
          ><span>{{ $t("mailbox.port") }}</span
          ><input
            type="number"
            min="1"
            max="65535"
            :value="value[kind]?.port"
            @input="endpoint(kind, 'port', number($event))"
        /></label>
        <label class="field"
          ><span>{{ $t("mailbox.serverName") }}</span
          ><input
            :value="value[kind]?.serverName ?? ''"
            autocomplete="off"
            @input="endpoint(kind, 'serverName', text($event))"
        /></label>
        <label class="field"
          ><span>TLS</span
          ><select
            :value="value[kind]?.tlsMode ?? ''"
            @change="
              endpoint(
                kind,
                'tlsMode',
                (text($event) || undefined) as EmailMailboxEndpoint['tlsMode'],
              )
            "
          >
            <option value=""></option>
            <option value="IMPLICIT">TLS</option>
            <option value="STARTTLS">STARTTLS</option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("mailbox.authMethod") }}</span
          ><select
            :value="value[kind]?.authMethod ?? ''"
            @change="
              endpoint(
                kind,
                'authMethod',
                (text($event) ||
                  undefined) as EmailMailboxEndpoint['authMethod'],
              )
            "
          >
            <option value=""></option>
            <option value="PASSWORD">{{ $t("mailbox.password") }}</option>
            <option value="OAUTHBEARER">OAuth Bearer</option>
          </select></label
        >
        <label v-for="(_, field) in credentialFields" :key="field" class="field"
          ><span>{{
            $t(`mailboxCredential.kinds.${credentialFields[field]}`)
          }}</span
          ><select
            :value="credentialKey(value[kind]?.[field])"
            @change="selectCredential(kind, field, $event)"
          >
            <option value=""></option>
            <option
              v-if="missingCredential(value[kind]?.[field], field)"
              :value="credentialKey(value[kind]?.[field])"
              disabled
            >
              {{ value[kind]?.[field]?.name }} ·
              {{ value[kind]?.[field]?.generation }} ({{
                $t("mailbox.notInPage")
              }})
            </option>
            <option
              v-for="item in credentialOptions(field)"
              :key="credentialKey(item)"
              :value="credentialKey(item)"
            >
              {{ item.name }} · {{ item.generation }}
            </option>
          </select></label
        >
      </div>
    </section>
    <details>
      <summary>{{ $t("mailbox.limits") }}</summary>
      <div class="mailbox-fields__grid">
        <label v-for="field in limitFields" :key="field" class="field"
          ><span>{{ $t(`mailbox.limitFields.${field}`) }}</span
          ><input
            type="number"
            min="0"
            :value="value.limits?.[field]"
            @input="
              set('limits', { ...value.limits, [field]: number($event) })
            "
        /></label>
      </div>
    </details>
    <section class="mailbox-fields__policies">
      <h4>{{ $t("mailbox.policies") }}</h4>
      <p>{{ $t("mailbox.policyHelp") }}</p>
      <div
        v-for="(item, index) in value.policies ?? []"
        :key="index"
        class="mailbox-fields__policy"
      >
        <label class="field"
          ><span>{{ $t("mailbox.operation") }}</span
          ><select
            :value="item.operation ?? ''"
            @change="policy(index, 'operation', $event)"
          >
            <option value=""></option>
            <option
              v-for="operation in operations"
              :key="operation"
              :value="operation"
            >
              {{ $t(`mailbox.operations.${operation}`) }}
            </option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("mailbox.approval") }}</span
          ><select
            :value="item.policy ?? ''"
            @change="policy(index, 'policy', $event)"
          >
            <option value=""></option>
            <option value="DENY">{{ $t("mailbox.deny") }}</option>
            <option value="ALLOW">{{ $t("mailbox.allow") }}</option>
            <option value="HUMAN_GATE">{{ $t("mailbox.gate") }}</option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("mailbox.fields.allowedFolders") }}</span
          ><VoiceTextarea
            :model-value="item.folders?.join('\n') ?? ''"
            :disabled="disabled"
            rows="2"
            @update:model-value="
              set(
                'policies',
                value.policies?.map((entry, position) =>
                  position === index
                    ? { ...entry, folders: strings($event) }
                    : entry,
                ),
              )
            "
          />
        </label>
        <button
          class="button"
          type="button"
          @click="
            set(
              'policies',
              value.policies?.filter((_, position) => position !== index),
            )
          "
        >
          {{ $t("common.delete") }}
        </button>
      </div>
      <button
        class="button"
        type="button"
        :disabled="
          disabled || (value.policies?.length ?? 0) >= operations.length
        "
        @click="addPolicy"
      >
        {{ $t("mailbox.addPolicy") }}
      </button>
    </section>
  </fieldset>
</template>

<style scoped>
.mailbox-fields {
  display: grid;
  gap: 16px;
  min-width: 0;
  border: 0;
  padding: 0;
  margin: 0;
}
.mailbox-fields__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.mailbox-fields__endpoint,
.mailbox-fields__policies {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 6px;
  min-width: 0;
}
.mailbox-fields__policy {
  display: grid;
  gap: 10px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}
h4 {
  margin: 0;
}
@media (max-width: 760px) {
  .mailbox-fields__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
