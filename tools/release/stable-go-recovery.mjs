import { fingerprint } from "./stable-inventory.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }

// Source spec восстанавливается только в памяти из exact before/after tuple.
// Никакие env или целые PodSpec не сохраняются в журнале восстановления.
export function prepareGoRecovery(current, item) {
  const recovery = item.recovery, specHash = fingerprint(current.spec), labels = current.metadata?.labels ?? {};
  requireValue(current.kind === "Deployment" && current.metadata?.namespace === "kodex-system" &&
    current.metadata.name === item.name && current.metadata.uid === item.uid && current.metadata.resourceVersion &&
    recovery && Array.isArray(recovery.inverse), "STABLE_RECOVERY_IDENTITY_DRIFT");
  const source = [item.beforeSpecSHA256, ...(item.quiescedBeforeSpecSHA256 ? [item.quiescedBeforeSpecSHA256] : [])].includes(specHash);
  const compiled = [item.afterSpecSHA256, recovery.quiescedAfterSpecSHA256].includes(specHash);
  requireValue((source || compiled) && fingerprint(labels) === (compiled ? recovery.afterLabelsSHA256 : recovery.beforeLabelsSHA256),
    "STABLE_RECOVERY_SPEC_DRIFT");
  let restored = structuredClone(current.spec);
  restored.replicas = compiled ? 2 : recovery.beforeReplicas;
  if (compiled) restored = applyReverseSpec(restored, recovery.inverse);
  requireValue(fingerprint(restored) === item.beforeSpecSHA256, "STABLE_RECOVERY_INVERSE_MISMATCH");
  const restoredLabels = { ...labels };
  if (recovery.beforeProfilePresent) restoredLabels["kodex.dev/local-profile"] = "hot-reload";
  else delete restoredLabels["kodex.dev/local-profile"];
  requireValue(fingerprint(restoredLabels) === recovery.beforeLabelsSHA256, "STABLE_RECOVERY_LABELS_MISMATCH");
  const unchanged = source && specHash === item.beforeSpecSHA256;
  const archivePause = item.name === "session-archive" && !unchanged;
  if (archivePause) restored.replicas = 0;
  requireValue(!archivePause || fingerprint(restored) === item.quiescedBeforeSpecSHA256, "STABLE_RECOVERY_PAUSE_MISMATCH");
  return { source, compiled, unchanged, archivePause, restoredSpec: restored, restoredLabels,
    restoredSpecSHA256: fingerprint(restored) };
}
