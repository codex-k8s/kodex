import { expect, it } from "vitest";
import {
  checkMutationRejection,
  KnownMutationRejection,
} from "./mutation-rejection";

const conflict = {
  status: 412,
  code: "VERSION_OR_STATE_CONFLICT",
  retryable: true,
};
const response = (status = 412, type = "application/problem+json") =>
  new Response(null, { status, headers: { "Content-Type": type } });

it.each([400, 412, 422] as const)(
  "распознаёт доказанный отказ %s до эффекта",
  (status) => {
    const error =
      status === 412
        ? conflict
        : { status, code: "INVALID_REQUEST", retryable: false };
    expect(() =>
      checkMutationRejection({ error, response: response(status) }),
    ).toThrow(KnownMutationRejection);
  },
);

it.each([
  { error: "proxy", response: response() },
  { error: conflict },
  { error: conflict, response: response(502) },
  { error: conflict, response: response(412, "text/html") },
  { error: conflict, response: response(412, "application/problem+json-fake") },
  { error: { ...conflict, status: 400 }, response: response() },
  { error: { ...conflict, code: "UNKNOWN" }, response: response() },
  { error: { ...conflict, retryable: "true" }, response: response() },
  { error: { status: 400, code: "INVALID_REQUEST" }, response: response(400) },
])("не объявляет proxy/неполный ответ известным отказом: %j", (result) => {
  expect(checkMutationRejection(result)).toBe(result);
});
