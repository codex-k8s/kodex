<script setup lang="ts">
import { Cpu, Network, Plus, Trash2 } from "@lucide/vue";

import {
  emptyRuntimeVolume,
  emptyRuntimeWebAccessRule,
  mandatoryRuntimeNetworkDestinations,
  runtimeResourceBounds,
  runtimeWebMethodsForMode,
  runtimeVolumeBounds,
} from "@/features/runtime/environment-form";
import type {
  RuntimeEnvironmentPolicyInput,
  RuntimeResourcePolicy,
  RuntimeVolumeInput,
  RuntimeWebAccessMode,
  RuntimeWebAccessRule,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  policy: RuntimeEnvironmentPolicyInput;
  disabled: boolean;
}>();
const emit = defineEmits<{
  "update:policy": [value: RuntimeEnvironmentPolicyInput];
}>();

const resourceFields = [
  {
    key: "cpuRequestMilli",
    label: "runtime.cpuRequest",
    range: "runtime.cpuRequestRange",
    step: 100,
  },
  {
    key: "cpuLimitMilli",
    label: "runtime.cpuLimit",
    range: "runtime.cpuLimitRange",
    step: 100,
  },
  {
    key: "memoryRequestMib",
    label: "runtime.memoryRequest",
    range: "runtime.memoryRequestRange",
    step: 128,
  },
  {
    key: "memoryLimitMib",
    label: "runtime.memoryLimit",
    range: "runtime.memoryLimitRange",
    step: 128,
  },
  {
    key: "ephemeralStorageRequestMib",
    label: "runtime.ephemeralStorageRequest",
    range: "runtime.ephemeralStorageRequestRange",
    step: 256,
  },
  {
    key: "ephemeralStorageLimitMib",
    label: "runtime.ephemeralStorageLimit",
    range: "runtime.ephemeralStorageLimitRange",
    step: 256,
  },
] as const;

function changeResource(key: keyof RuntimeResourcePolicy, event: Event): void {
  if (props.disabled) return;
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  emit("update:policy", {
    ...props.policy,
    resources: { ...props.policy.resources, [key]: Number(target.value) },
  });
}

function changeVolume(
  index: number,
  key: keyof RuntimeVolumeInput,
  event: Event,
): void {
  if (props.disabled) return;
  const target = event.target;
  if (
    !(target instanceof HTMLInputElement || target instanceof HTMLSelectElement)
  )
    return;
  emit("update:policy", {
    ...props.policy,
    volumes: props.policy.volumes.map((volume, current) =>
      current === index
        ? {
            ...volume,
            [key]: key === "sizeMib" ? Number(target.value) : target.value,
          }
        : volume,
    ),
  });
}

function addVolume(): void {
  if (
    props.disabled ||
    props.policy.volumes.length >= runtimeVolumeBounds.maxItems
  )
    return;
  emit("update:policy", {
    ...props.policy,
    volumes: [...props.policy.volumes, emptyRuntimeVolume()],
  });
}

function removeVolume(index: number): void {
  if (props.disabled) return;
  emit("update:policy", {
    ...props.policy,
    volumes: props.policy.volumes.filter((_, current) => current !== index),
  });
}

function changeWebAccessMode(event: Event): void {
  if (props.disabled) return;
  const target = event.target;
  if (!(target instanceof HTMLSelectElement)) return;
  const mode = target.value as RuntimeWebAccessMode;
  const allowedMethods = runtimeWebMethodsForMode(mode);
  const rules =
    mode === "NONE" || mode === "FULL_PUBLIC"
      ? []
      : props.policy.webAccess.rules.length
        ? props.policy.webAccess.rules.map((rule) => {
            const retained = rule.httpMethods.filter((method) =>
              allowedMethods.includes(method),
            );
            return {
              ...rule,
              httpMethods: retained.length ? retained : [...allowedMethods],
            };
          })
        : [emptyRuntimeWebAccessRule(mode)];
  emit("update:policy", { ...props.policy, webAccess: { mode, rules } });
}

function toggleWebMethod(
  index: number,
  method: RuntimeWebAccessRule["httpMethods"][number],
  event: Event,
): void {
  if (props.disabled) return;
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  emit("update:policy", {
    ...props.policy,
    webAccess: {
      ...props.policy.webAccess,
      rules: props.policy.webAccess.rules.map((rule, current) => {
        if (current !== index) return rule;
        const selected = target.checked
          ? [...rule.httpMethods, method]
          : rule.httpMethods.filter((value) => value !== method);
        return {
          ...rule,
          httpMethods: runtimeWebMethodsForMode(
            props.policy.webAccess.mode,
          ).filter((value) => selected.includes(value)),
        };
      }),
    },
  });
}

function addWebRule(): void {
  if (props.disabled || props.policy.webAccess.rules.length >= 64) return;
  emit("update:policy", {
    ...props.policy,
    webAccess: {
      ...props.policy.webAccess,
      rules: [
        ...props.policy.webAccess.rules,
        emptyRuntimeWebAccessRule(props.policy.webAccess.mode),
      ],
    },
  });
}

function removeWebRule(index: number): void {
  if (props.disabled) return;
  emit("update:policy", {
    ...props.policy,
    webAccess: {
      ...props.policy.webAccess,
      rules: props.policy.webAccess.rules.filter(
        (_, current) => current !== index,
      ),
    },
  });
}

function changeWebRule(index: number, event: Event): void {
  if (props.disabled) return;
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  emit("update:policy", {
    ...props.policy,
    webAccess: {
      ...props.policy.webAccess,
      rules: props.policy.webAccess.rules.map((rule, current) =>
        current === index ? { ...rule, domainPattern: target.value } : rule,
      ),
    },
  });
}
</script>

<template>
  <div class="runtime-policy-fields">
    <section class="policy-group">
      <div class="section-header">
        <div>
          <h3>{{ $t("runtime.resources") }}</h3>
          <p>{{ $t("runtime.resourcesHelp") }}</p>
        </div>
        <Cpu :size="20" aria-hidden="true" />
      </div>
      <div class="resource-grid">
        <label v-for="field in resourceFields" :key="field.key" class="field">
          <span>{{ $t(field.label) }}</span>
          <input
            type="number"
            :name="`runtime-resource-${field.key}`"
            :value="policy.resources[field.key]"
            :min="runtimeResourceBounds[field.key].min"
            :max="runtimeResourceBounds[field.key].max"
            :step="field.step"
            :disabled="disabled"
            @input="changeResource(field.key, $event)"
          />
          <small>{{ $t(field.range) }}</small>
        </label>
      </div>
    </section>

    <section class="policy-group">
      <div class="section-header">
        <div>
          <h3>{{ $t("runtime.ephemeralVolumes") }}</h3>
          <p>{{ $t("runtime.ephemeralVolumesHelp") }}</p>
        </div>
        <button
          class="button"
          type="button"
          :disabled="
            disabled || policy.volumes.length >= runtimeVolumeBounds.maxItems
          "
          @click="addVolume"
        >
          <Plus :size="15" aria-hidden="true" />
          {{ $t("runtime.addVolume") }}
        </button>
      </div>
      <div v-if="policy.volumes.length" class="volume-list">
        <article
          v-for="(volume, index) in policy.volumes"
          :key="index"
          class="volume-row"
        >
          <label class="field">
            <span>{{ $t("common.name") }}</span>
            <input
              :value="volume.name"
              :name="`runtime-volume-name-${index}`"
              :disabled="disabled"
              placeholder="workspace-cache"
              @input="changeVolume(index, 'name', $event)"
            />
          </label>
          <label class="field">
            <span>{{ $t("runtime.volumeKind") }}</span>
            <select
              :value="volume.kind"
              :name="`runtime-volume-kind-${index}`"
              :disabled="disabled"
              @change="changeVolume(index, 'kind', $event)"
            >
              <option value="EPHEMERAL_DISK">
                {{ $t("runtime.volumeKindLabel.EPHEMERAL_DISK") }}
              </option>
              <option value="EPHEMERAL_MEMORY">
                {{ $t("runtime.volumeKindLabel.EPHEMERAL_MEMORY") }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>{{ $t("runtime.volumeSize") }}</span>
            <input
              type="number"
              :name="`runtime-volume-size-${index}`"
              :value="volume.sizeMib"
              :min="runtimeVolumeBounds.minSizeMib"
              :max="runtimeVolumeBounds.maxSizeMib"
              step="16"
              :disabled="disabled"
              @input="changeVolume(index, 'sizeMib', $event)"
            />
          </label>
          <div class="volume-mount">
            <span>{{ $t("runtime.mountPath") }}</span>
            <code>{{
              volume.name ? `/workspace/.kodex/volumes/${volume.name}` : "—"
            }}</code>
          </div>
          <button
            class="icon-button icon-button--danger"
            type="button"
            :aria-label="$t('common.delete')"
            :disabled="disabled"
            @click="removeVolume(index)"
          >
            <Trash2 :size="16" aria-hidden="true" />
          </button>
        </article>
      </div>
      <p v-else class="secondary-text">
        {{ $t("runtime.noEphemeralVolumes") }}
      </p>
    </section>

    <section class="policy-group">
      <div class="section-header">
        <div>
          <h3>{{ $t("runtime.networkPolicy") }}</h3>
          <p>{{ $t("runtime.networkPolicyHelp") }}</p>
        </div>
        <Network :size="20" aria-hidden="true" />
      </div>
      <div class="destination-list">
        <article
          v-for="destination in mandatoryRuntimeNetworkDestinations"
          :key="destination"
          class="destination-row"
        >
          <div>
            <strong>{{
              $t(`runtime.networkDestination.${destination}`)
            }}</strong>
            <p>{{ $t(`runtime.networkDestinationHelp.${destination}`) }}</p>
          </div>
          <StatusBadge
            state="REQUIRED"
            :label="$t('runtime.mandatoryDestination')"
          />
        </article>
      </div>
      <div class="web-access-editor">
        <label class="field">
          <span>{{ $t("runtime.webAccessMode") }}</span>
          <select
            name="runtime-web-access-mode"
            :value="policy.webAccess.mode"
            :disabled="disabled"
            @change="changeWebAccessMode"
          >
            <option value="NONE">
              {{ $t("runtime.webAccessModeLabel.NONE") }}
            </option>
            <option value="ALLOWLIST_READ_ONLY">
              {{ $t("runtime.webAccessModeLabel.ALLOWLIST_READ_ONLY") }}
            </option>
            <option value="ALLOWLIST_FULL">
              {{ $t("runtime.webAccessModeLabel.ALLOWLIST_FULL") }}
            </option>
            <option value="FULL_PUBLIC">
              {{ $t("runtime.webAccessModeLabel.FULL_PUBLIC") }}
            </option>
          </select>
        </label>
        <p class="secondary-text">
          {{ $t(`runtime.webAccessModeHelp.${policy.webAccess.mode}`) }}
        </p>
        <template v-if="policy.webAccess.mode.startsWith('ALLOWLIST')">
          <article
            v-for="(rule, index) in policy.webAccess.rules"
            :key="index"
            class="web-rule-row"
          >
            <label class="field web-rule-domain">
              <span>{{ $t("runtime.webAccessDomain") }}</span>
              <input
                :name="`runtime-web-domain-${index}`"
                :value="rule.domainPattern"
                placeholder="api.example.com или **.example.com"
                :disabled="disabled"
                @input="changeWebRule(index, $event)"
              />
            </label>
            <div class="web-rule-transport">
              <span>HTTPS</span><code>443</code>
            </div>
            <div
              class="method-list"
              :aria-label="$t('runtime.webAccessMethods')"
            >
              <span class="method-list__label">{{
                $t("runtime.webAccessMethods")
              }}</span>
              <label
                v-for="method in runtimeWebMethodsForMode(
                  policy.webAccess.mode,
                )"
                :key="method"
                class="method-option"
              >
                <input
                  type="checkbox"
                  :checked="rule.httpMethods.includes(method)"
                  :disabled="disabled"
                  @change="toggleWebMethod(index, method, $event)"
                />
                <code>{{ method }}</code>
              </label>
            </div>
            <button
              class="icon-button icon-button--danger"
              type="button"
              :aria-label="$t('common.delete')"
              :disabled="disabled"
              @click="removeWebRule(index)"
            >
              <Trash2 :size="16" aria-hidden="true" />
            </button>
          </article>
          <button
            class="button web-rule-add"
            type="button"
            :disabled="disabled || policy.webAccess.rules.length >= 64"
            @click="addWebRule"
          >
            <Plus :size="15" aria-hidden="true" />
            {{ $t("runtime.addWebAccessRule") }}
          </button>
        </template>
      </div>
    </section>
  </div>
</template>

<style scoped>
.runtime-policy-fields,
.policy-group,
.volume-list,
.destination-list {
  display: grid;
  gap: 12px;
}
.policy-group {
  padding-bottom: 18px;
  border-bottom: 1px solid var(--hairline);
}
.policy-group:last-of-type {
  padding-bottom: 0;
  border-bottom: 0;
}
.section-header {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}
.section-header h3,
.section-header p {
  margin: 0;
}
.section-header p,
.secondary-text {
  color: var(--text-secondary);
}
.resource-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.resource-grid .field small,
.volume-mount span {
  color: var(--text-secondary);
}
.volume-row {
  display: grid;
  grid-template-columns:
    minmax(160px, 1fr) minmax(150px, 0.72fr) minmax(120px, 0.5fr)
    minmax(210px, 1fr) 36px;
  gap: 10px;
  align-items: end;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.volume-mount {
  display: grid;
  min-width: 0;
  gap: 7px;
  align-self: center;
}
.volume-mount code {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.destination-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.destination-row p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}
.web-access-editor {
  display: grid;
  gap: 10px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.web-rule-row {
  display: grid;
  grid-template-columns: minmax(240px, 1.2fr) auto minmax(360px, 1fr) 36px;
  align-items: end;
  gap: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--hairline);
}
.web-rule-transport {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 38px;
}
.method-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 10px;
  margin: 0;
  padding: 0;
}
.method-list__label {
  width: 100%;
  color: var(--text-secondary);
}
.method-option {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
}
.method-option input {
  margin: 0;
}
.method-option code {
  padding: 3px 6px;
  border-radius: 5px;
  background: var(--surface-subtle);
  font-size: 12px;
}
.web-rule-add {
  justify-self: start;
}
@media (max-width: 1040px) {
  .volume-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 700px) {
  .resource-grid,
  .volume-row {
    grid-template-columns: 1fr;
  }
}
</style>
