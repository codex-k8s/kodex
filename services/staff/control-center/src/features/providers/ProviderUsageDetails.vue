<script setup lang="ts">
import type { ProviderAccountUsage } from "@/shared/api/generated/openapi/types.gen";
defineProps<{ usage?: ProviderAccountUsage; compact?: boolean }>();
const dimensions = [
  "lifecycle",
  "credential",
  "providerHealth",
  "modelCompatibility",
  "capacity",
  "actorEligibility",
] as const;
</script>
<template>
  <component :is="compact ? 'div' : 'details'" class="provider-usage">
    <summary v-if="!compact">{{ $t("providerUsage.title") }}</summary>
    <template v-if="usage">
      <p v-if="usage.context">
        {{ $t("providerUsage.selection") }}:
        {{
          $t(
            usage.eligibleForSelection
              ? "providerUsage.allowed"
              : "providerUsage.denied",
          )
        }}
        · {{ $t("providerUsage.submit") }}:
        {{
          $t(
            usage.allowedToSubmit
              ? "providerUsage.allowed"
              : "providerUsage.denied",
          )
        }}
      </p>
      <dl>
        <template v-for="dimension in dimensions" :key="dimension">
          <dt>{{ $t(`providerUsage.dimensions.${dimension}`) }}</dt>
          <dd>
            {{ $t(`providerUsage.states.${usage[dimension].state}`) }}:
            {{ $t(`providerUsage.reasons.${usage[dimension].reason}`) }}
            <small v-if="usage[dimension].remediation !== 'NONE'">{{
              $t(`providerUsage.remediation.${usage[dimension].remediation}`)
            }}</small>
          </dd>
        </template>
      </dl>
      <template v-if="!compact">
        <p>{{ $t("providerUsage.healthScope") }}</p>
        <p v-if="usage.providerHealthObservedAt">
          {{ $t("providerUsage.observed") }}:
          <time :datetime="usage.providerHealthObservedAt">{{
            usage.providerHealthObservedAt
          }}</time>
        </p>
        <p v-if="usage.providerHealthExpiresAt">
          {{ $t("providerUsage.expires") }}:
          <time :datetime="usage.providerHealthExpiresAt">{{
            usage.providerHealthExpiresAt
          }}</time>
        </p>
        <p>
          {{
            $t("providerUsage.capacity", {
              active: usage.activeExecutions,
              maximum: usage.maximumConcurrentExecutions,
            })
          }}
        </p>
        <p>
          {{ $t("providerUsage.expires") }}:
          <time :datetime="usage.expiresAt">{{ usage.expiresAt }}</time>
        </p>
      </template>
    </template>
    <p v-else>{{ $t("providerUsage.missing") }}</p>
  </component>
</template>
<style scoped>
.provider-usage {
  min-width: 0;
  font-size: 0.8rem;
  overflow-wrap: anywhere;
}
summary {
  cursor: pointer;
}
dl {
  display: grid;
  gap: 0.25rem;
}
dt {
  font-weight: 600;
}
dd {
  margin: 0 0 0.5rem;
}
small {
  display: block;
}
p {
  margin: 0.35rem 0;
}
</style>
