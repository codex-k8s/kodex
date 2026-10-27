import test from "node:test";
import assert from "node:assert/strict";
import { ownerSnapshot, sameOwnerPins } from "./runtime-policy-refactor-idle.mjs";

const now = Date.parse("2026-10-27T12:00:00Z");
const state = () => ({ at: "2026-10-27T11:59:59.123456Z", openBuilds: 0, pendingAdmissions: 0,
  pendingPromotions: 0, unrequestedAcceptedArtifacts: 2, activeRuntimeRuns: 0, claimedRuntimeLeases: 0,
  promotedArtifactCount: 8, promotedPinsSHA256: "a".repeat(64) });

test("снимок не заявляет fencing и допускает незаказанные артефакты", () => {
  assert.equal(ownerSnapshot(state(), now).unrequestedAcceptedArtifacts, 2);
  const result = sameOwnerPins(state(), { ...state(), at: "2026-10-27T12:00:00+00:00" }, now);
  assert.equal(result.fencing, "NOT_PROVEN");
  assert.equal(result.apply, "NOT_AUTHORIZED");
});

test("активная работа, stale или повреждённый снимок закрыто отклоняются", () => {
  const cases = [
    { openBuilds: 1 }, { pendingAdmissions: 1 }, { pendingPromotions: 1 }, { activeRuntimeRuns: 1 },
    { claimedRuntimeLeases: 1 }, { promotedArtifactCount: -1 }, { promotedPinsSHA256: "wrong" },
    { unrequestedAcceptedArtifacts: -1 }, { unrequestedAcceptedArtifacts: 1.5 }, { extra: 1 },
    { at: "2026-10-27T11:59:30Z" }, { at: "2026-10-27T12:00:06Z" },
    { at: "2026-02-31T00:00:00Z" }, { at: "2026-10-27T12:00:00" }, { at: "2026-10-27T24:00:00Z" },
  ];
  for (const change of cases) assert.throws(() => ownerSnapshot({ ...state(), ...change }, now));
  const missing = state(); delete missing.openBuilds;
  assert.throws(() => ownerSnapshot(missing, now));
  assert.throws(() => ownerSnapshot(state(), NaN));
});

test("изменение promoted pins и незаказанного количества отклоняется", () => {
  for (const change of [{ promotedArtifactCount: 9 }, { promotedPinsSHA256: "b".repeat(64) },
    { unrequestedAcceptedArtifacts: 3 }, { at: "2026-10-27T11:59:58Z" }]) {
    assert.throws(() => sameOwnerPins(state(), { ...state(), ...change }, now), /OWNER_PINS_DRIFT/);
  }
});
