import { describe, expect, it } from "vitest";
import { SessionBootstrapCorrelator } from "./session-bootstrap-observer";

const address = "https://control.example/api/v1/bootstrap";
const id = "00000000-0000-4000-8000-000000000001:1";
function setup() {
  const proof = new SessionBootstrapCorrelator<object>(address),
    request = {};
  proof.request(request, address, "GET", id);
  proof.observe({ phase: "start", id, intentional: false, observedAt: 100 });
  return { proof, request };
}
describe("SessionBootstrapCorrelator", () => {
  it("подтверждает exact native signal до failure даже при поздней доставке binding", () => {
    const { proof, request } = setup();
    proof.failed(request, 200);
    proof.observe({ phase: "reject", id, intentional: true, observedAt: 200 });
    proof.observe({ phase: "abort", id, intentional: true, observedAt: 199 });
    expect(proof.cancelled(request)).toBe(true);
    expect(JSON.stringify(proof.snapshot())).not.toContain(address);
    expect(JSON.stringify(proof.snapshot())).not.toContain(id);
  });
  it.each([false, true])(
    "не подавляет timeout или позднюю отмену: %s",
    (late) => {
      const { proof, request } = setup();
      proof.failed(request, 200);
      proof.observe({
        phase: "reject",
        id,
        intentional: true,
        observedAt: 200,
      });
      proof.observe({
        phase: "abort",
        id,
        intentional: late,
        observedAt: late ? 201 : 199,
      });
      expect(proof.cancelled(request)).toBe(false);
    },
  );
  it("не связывает соседний request, повторную identity, POST или query по URL/порядку", () => {
    for (const [url, method] of [
      [address + "?query=1", "GET"],
      [address, "POST"],
      ["https://foreign.example/api/v1/bootstrap", "GET"],
    ]) {
      const { proof, request } = setup(),
        other = {};
      if (!url || !method) throw new Error("Invalid fixture");
      proof.request(other, url, method, id);
      proof.failed(other, 200);
      proof.observe({ phase: "abort", id, intentional: true, observedAt: 150 });
      expect(proof.cancelled(other)).toBe(false);
      proof.request({}, address, "GET", id);
      proof.failed(request, 200);
      proof.observe({
        phase: "reject",
        id,
        intentional: true,
        observedAt: 200,
      });
      expect(proof.cancelled(request)).toBe(false);
    }
  });
  it("native network rejection не становится отменой даже при наличии signal", () => {
    const { proof, request } = setup();
    proof.failed(request, 200);
    proof.observe({ phase: "abort", id, intentional: true, observedAt: 199 });
    proof.observe({ phase: "reject", id, intentional: false, observedAt: 200 });
    expect(proof.cancelled(request)).toBe(false);
  });
  it("overflow закрыто блокирует подтверждение", () => {
    const { proof, request } = setup();
    for (let i = 0; i < 4097; i++)
      proof.observe({ phase: "abort", id, intentional: true, observedAt: 150 });
    proof.failed(request, 200);
    proof.observe({ phase: "reject", id, intentional: true, observedAt: 200 });
    expect(proof.cancelled(request)).toBe(false);
    expect(proof.snapshot().events).toBe(4096);
    expect(proof.snapshot().overflow).toBeGreaterThan(0);
  });
});
