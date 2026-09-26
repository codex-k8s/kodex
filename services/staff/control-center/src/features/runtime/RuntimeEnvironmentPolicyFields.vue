<script setup lang="ts">
import {
  CircleAlert,
  Cpu,
  Network,
  Plus,
  ShieldCheck,
  Trash2,
} from "@lucide/vue";

import {
  emptyRuntimeVolume,
  mandatoryRuntimeNetworkDestinations,
  runtimeResourceBounds,
  runtimeVolumeBounds,
  setRuntimeKubernetesAccess,
} from "@/features/runtime/environment-form";
import type {
  RuntimeEnvironmentPolicyInput,
  RuntimeResourcePolicy,
  RuntimeVolumeInput,
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

function changeAccess(event: Event): void {
  if (props.disabled) return;
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  const next: RuntimeEnvironmentPolicyInput = {
    ...props.policy,
    resources: { ...props.policy.resources },
    volumes: [...props.policy.volumes],
    networkDestinations: [...props.policy.networkDestinations],
  };
  setRuntimeKubernetesAccess(
    next,
    target.checked ? "READ_OWN_EXECUTION" : "NONE",
  );
  emit("update:policy", next);
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
        <article class="destination-row">
          <div>
            <strong>{{
              $t("runtime.networkDestination.KUBERNETES_API")
            }}</strong>
            <p>{{ $t("runtime.networkDestinationHelp.KUBERNETES_API") }}</p>
          </div>
          <StatusBadge
            :state="
              policy.kubernetesAccess === 'READ_OWN_EXECUTION'
                ? 'AVAILABLE'
                : 'DISABLED'
            "
            :label="
              policy.kubernetesAccess === 'READ_OWN_EXECUTION'
                ? $t('runtime.scopedAccessEnabled')
                : $t('common.disabled')
            "
          />
        </article>
      </div>
    </section>

    <section class="policy-group">
      <div class="section-header">
        <div>
          <h3>{{ $t("runtime.kubernetesRbac") }}</h3>
          <p>{{ $t("runtime.kubernetesRbacHelp") }}</p>
        </div>
        <ShieldCheck :size="20" aria-hidden="true" />
      </div>
      <label class="access-toggle">
        <input
          type="checkbox"
          name="runtime-read-own-execution"
          :checked="policy.kubernetesAccess === 'READ_OWN_EXECUTION'"
          :disabled="disabled"
          @change="changeAccess"
        />
        <span>
          <strong>{{ $t("runtime.readOwnExecution") }}</strong>
          <small>{{ $t("runtime.readOwnExecutionHelp") }}</small>
        </span>
      </label>
      <p class="boundary-note" role="note">
        <CircleAlert :size="17" aria-hidden="true" />
        {{ $t("runtime.kubernetesAccessBoundary") }}
      </p>
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
.access-toggle small,
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
.access-toggle {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  cursor: pointer;
}
.access-toggle > span {
  display: grid;
  gap: 4px;
}
.boundary-note {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px 12px;
  border: 1px solid var(--warning);
  border-radius: 7px;
  background: var(--warning-soft);
  color: var(--warning);
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
