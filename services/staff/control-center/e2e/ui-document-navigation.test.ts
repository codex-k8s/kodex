import { expect, it } from "vitest";
import { DocumentNavigation } from "./ui-document-navigation";
const oldDocument = "11111111-1111-4111-8111-111111111111",
  newDocument = "22222222-2222-4222-8222-222222222222";
it("tracks exact pending request of replaced document without requiring JS fetch identity", () => {
  const c = new DocumentNavigation<object>(),
    r = {};
  c.document(oldDocument, 1);
  c.request(r, true, 2);
  const intent = c.begin("GOTO", 3);
  c.terminal(r, 4);
  expect(c.confirmed(r)).toBe(false);
  c.document(newDocument, 5);
  c.end(intent, true, 6);
  expect(c.confirmed(r)).toBe(true);
  expect(c.evidence(r)).toMatchObject({
    documentRequestObserved: true,
    documentNavigationIntent: true,
    documentNavigationCommitted: true,
    documentNavigationWindow: true,
  });
  expect(JSON.stringify(c.evidence(r))).not.toContain(oldDocument);
});
it("never accepts prior terminal, future request, foreign frame, unknown document, failed/same-document navigation or late failure", () => {
  for (const variant of [
    "prior",
    "future",
    "foreign",
    "unknown",
    "failed",
    "same",
    "late",
    "overlap",
  ]) {
    const c = new DocumentNavigation<object>(),
      r = {};
    if (variant !== "unknown") c.document(oldDocument, 1);
    if (variant !== "future") c.request(r, variant !== "foreign", 2);
    if (variant === "prior") c.terminal(r, 2);
    const intent = c.begin("GOTO", 3);
    if (variant === "overlap") c.begin("RELOAD", 4);
    if (variant !== "same") c.document(newDocument, 5);
    if (variant === "future") c.request(r, true, 5);
    if (variant !== "prior") c.terminal(r, variant === "late" ? 8 : 6);
    c.end(intent, variant !== "failed" && variant !== "same", 7);
    expect(c.confirmed(r), variant).toBe(false);
  }
});
it("successful page close is bounded and cannot retroactively authorize an older failure", () => {
  const c = new DocumentNavigation<object>(),
    r = {},
    prior = {};
  c.document(oldDocument, 1);
  c.request(r, true, 2);
  c.request(prior, true, 2);
  c.terminal(prior, 3);
  const intent = c.begin("CLOSE", 4);
  c.terminal(r, 5);
  c.end(intent, true, 6);
  expect(c.confirmed(r)).toBe(true);
  expect(c.confirmed(prior)).toBe(false);
});
it("bounded navigation overflow closes request acceptance", () => {
  const c = new DocumentNavigation<object>(),
    r = {};
  c.document(oldDocument, 1);
  c.request(r, true, 2);
  const intent = c.begin("GOTO", 3);
  c.terminal(r, 4);
  c.document(newDocument, 5);
  c.end(intent, true, 6);
  for (let i = 0; i < 512; i++) {
    const next = c.begin("GOTO", 7 + i);
    c.end(next, false, 8 + i);
  }
  expect(c.overflowCount()).toBe(1);
  expect(c.confirmed(r)).toBe(false);
});

it("same-document attempt does not poison a later full navigation and late failure remains closed", () => {
  const c = new DocumentNavigation<object>(),
    r = {};
  c.document(oldDocument, 1);
  c.request(r, true, 2);
  const same = c.begin("GOTO", 3);
  c.end(same, false, 4, true);
  const full = c.begin("GOTO", 5);
  c.terminal(r, 6);
  c.document(newDocument, 7);
  c.end(full, true, 8);
  expect(c.confirmed(r)).toBe(true);
});
it("failed navigation with unknown resulting document invalidates subsequent provenance", () => {
  const c = new DocumentNavigation<object>(),
    r = {};
  c.document(oldDocument, 1);
  const failed = c.begin("GOTO", 2);
  c.end(failed, false, 3);
  c.request(r, true, 4);
  const next = c.begin("GOTO", 5);
  c.terminal(r, 6);
  c.document(newDocument, 7);
  c.end(next, true, 8);
  expect(c.confirmed(r)).toBe(false);
});

it("same-millisecond late failure is rejected by observation order", () => {
  const c = new DocumentNavigation<object>(),
    r = {};
  c.document(oldDocument, 1);
  c.request(r, true, 2);
  const intent = c.begin("GOTO", 3);
  c.document(newDocument, 4);
  c.end(intent, true, 5);
  c.terminal(r, 5);
  expect(c.confirmed(r)).toBe(false);
});
