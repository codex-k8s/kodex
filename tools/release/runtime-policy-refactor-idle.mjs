import { requireIdle } from "./runner-policy-model.mjs";

const keys = ["at", "openBuilds", "pendingAdmissions", "pendingPromotions", "unrequestedAcceptedArtifacts",
  "activeRuntimeRuns", "claimedRuntimeLeases", "promotedArtifactCount", "promotedPinsSHA256"];

function validTimestamp(value) {
  if (typeof value !== "string") return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,6})?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!match) return false;
  const [, year, month, day, hour, minute, second, zone, , zoneHour, zoneMinute] = match;
  const y = Number(year), m = Number(month), leap = y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0);
  const lengths = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return y > 0 && m >= 1 && m <= 12 && Number(day) >= 1 && Number(day) <= lengths[m - 1] &&
    Number(hour) <= 23 && Number(minute) <= 59 && Number(second) <= 59 &&
    (zone === "Z" || Number(zoneHour) <= 23 && Number(zoneMinute) <= 59);
}

// Снимок не создаёт fence и сам по себе не доказывает происхождение owner readback.
export function ownerSnapshot(value, now = Date.now()) {
  if (!value || typeof value !== "object" || Array.isArray(value) ||
      Object.keys(value).length !== keys.length || !keys.every(key => Object.hasOwn(value, key)) ||
      !Number.isSafeInteger(now) || now <= 0 || !validTimestamp(value.at) ||
      !Number.isSafeInteger(value.unrequestedAcceptedArtifacts) || value.unrequestedAcceptedArtifacts < 0) {
    throw new Error("OWNER_SNAPSHOT_INVALID");
  }
  requireIdle(value, now);
  return { at: value.at, promotedArtifactCount: value.promotedArtifactCount,
    promotedPinsSHA256: value.promotedPinsSHA256, unrequestedAcceptedArtifacts: value.unrequestedAcceptedArtifacts };
}

// Сравнение не удерживает idle и не разрешает запись без отдельного fencing.
export function sameOwnerPins(before, after, now = Date.now()) {
  const first = ownerSnapshot(before, now), second = ownerSnapshot(after, now);
  if (Date.parse(first.at) > Date.parse(second.at) ||
      first.promotedArtifactCount !== second.promotedArtifactCount ||
      first.promotedPinsSHA256 !== second.promotedPinsSHA256 ||
      first.unrequestedAcceptedArtifacts !== second.unrequestedAcceptedArtifacts) {
    throw new Error("OWNER_PINS_DRIFT");
  }
  return { status: "PINS_MATCH", beforeAt: first.at, afterAt: second.at,
    promotedArtifactCount: first.promotedArtifactCount, promotedPinsSHA256: first.promotedPinsSHA256,
    fencing: "NOT_PROVEN", apply: "NOT_AUTHORIZED" };
}
