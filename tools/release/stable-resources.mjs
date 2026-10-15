// Ресурсный профиль берётся из canonical Deployment репозитория. Runtime
// credentials и полный PodSpec не являются частью профиля или его отчёта.
function requireValue(value, code) { if (!value) throw Error(code); }

export function resourceQuantity(value, name) {
  requireValue(typeof value === "string" && value.length <= 32, "STABLE_RESOURCE_QUANTITY_INVALID");
  const match = name === "cpu" ? /^(\d+(?:\.\d{1,3})?)(m?)$/.exec(value) : /^(\d+)(Ki|Mi|Gi|Ti)?$/.exec(value);
  requireValue(match, "STABLE_RESOURCE_QUANTITY_INVALID");
  const result = name === "cpu" ? Number(match[1]) * (match[2] ? 1 : 1000) :
    Number(match[1]) * ({ Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4 }[match[2]] ?? 1);
  requireValue(Number.isSafeInteger(result) && result > 0, "STABLE_RESOURCE_QUANTITY_INVALID");
  return result;
}

export function boundedResources(profile, previous = {}) {
  requireValue(profile && typeof profile === "object" && !Array.isArray(profile) && previous &&
    [profile, previous].every(r => Object.keys(r).every(k => ["requests", "limits"].includes(k)) &&
      Object.values(r).every(v => v && typeof v === "object" && !Array.isArray(v) &&
        Object.keys(v).every(k => ["cpu", "memory", "ephemeral-storage"].includes(k)))), "STABLE_RESOURCE_PROFILE_INVALID");
  requireValue(profile.requests && profile.limits && ["cpu", "memory"].every(k =>
    Object.hasOwn(profile.requests, k) && Object.hasOwn(profile.limits, k)), "STABLE_RESOURCE_BOUND_REQUIRED");
  for (const key of new Set([...Object.keys(profile.requests), ...Object.keys(profile.limits)])) {
    const request = Object.hasOwn(profile.requests, key) ? resourceQuantity(profile.requests[key], key) : 0;
    const limit = Object.hasOwn(profile.limits, key) ? resourceQuantity(profile.limits[key], key) : null;
    requireValue(limit !== null && request <= limit, "STABLE_RESOURCE_BOUND_INVALID");
  }
  // Не снимать существующий предел scratch storage незаметно при переходе.
  requireValue(!previous.limits?.["ephemeral-storage"] || profile.limits["ephemeral-storage"],
    "STABLE_RESOURCE_STORAGE_BOUND_LOST");
  return structuredClone(profile);
}

export function podResourceBudget(pod) {
  requireValue(pod && Array.isArray(pod.containers) && pod.containers.length > 0 && !pod.resources,
    "STABLE_POD_RESOURCE_PROFILE_INVALID");
  const all = [...pod.containers, ...(pod.initContainers ?? [])];
  requireValue(all.length <= 64 && new Set(all.map(c => c.name)).size === all.length,
    "STABLE_POD_RESOURCE_PROFILE_INVALID");
  const result = {};
  for (const resource of ["cpu", "memory"]) {
    const effective = bound => {
      const amount = c => c.resources?.[bound]?.[resource] === undefined ?
        bound === "limits" ? null : 0 : resourceQuantity(c.resources[bound][resource], resource);
      const sum = values => values.includes(null) ? null : values.reduce((a, b) => a + b, 0);
      const max = values => values.includes(null) ? null : Math.max(...values);
      const runningInit = [];
      const stages = [];
      for (const c of pod.initContainers ?? []) {
        if (c.restartPolicy === "Always") runningInit.push(amount(c));
        else requireValue(c.restartPolicy === undefined, "STABLE_INIT_RESOURCE_PROFILE_INVALID");
        stages.push(sum([...runningInit, ...(c.restartPolicy === "Always" ? [] : [amount(c)])]));
      }
      stages.push(sum([...runningInit, ...pod.containers.map(amount)]));
      const overhead = pod.overhead?.[resource] === undefined ? 0 : resourceQuantity(pod.overhead[resource], resource);
      const value = max(stages);
      return value === null ? null : value + overhead;
    };
    result[resource === "cpu" ? "cpuMilli" : "memoryBytes"] = { requests: effective("requests"), limits: effective("limits") };
  }
  return result;
}
