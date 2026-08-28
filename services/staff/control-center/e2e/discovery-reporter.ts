import {
  chmodSync,
  mkdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";

import type {
  FullResult,
  Reporter,
  TestCase,
  TestResult,
} from "@playwright/test/reporter";

interface DiscoveryResult {
  readonly annotations: readonly string[];
  readonly durationMs: number;
  readonly errors: readonly string[];
  readonly project: string;
  readonly status: TestResult["status"];
  readonly title: string;
}

class DiscoveryReporter implements Reporter {
  private readonly results: DiscoveryResult[] = [];
  private startedAt = new Date().toISOString();

  onBegin(): void {
    this.startedAt = new Date().toISOString();
  }

  onTestEnd(test: TestCase, result: TestResult): void {
    this.results.push({
      annotations: test.annotations
        .map((annotation) => annotation.description)
        .filter((description): description is string => Boolean(description)),
      project: test.parent.project()?.name ?? "unknown",
      title: test.titlePath().slice(1).join(" > "),
      status: result.status,
      durationMs: result.duration,
      errors: result.errors.map((error) => safeError(error.message)),
    });
  }

  onEnd(result: FullResult): void {
    if (process.env.KODEX_E2E_CHECK_ONLY === "1") return;
    const path = reportPath();
    const temporaryPath = `${path}.${String(process.pid)}.tmp`;
    mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
    const summary = this.results.reduce<Record<string, number>>(
      (counts, item) => {
        counts[item.status] = (counts[item.status] ?? 0) + 1;
        return counts;
      },
      {},
    );
    const report = {
      version: 1,
      startedAt: this.startedAt,
      finishedAt: new Date().toISOString(),
      status: result.status,
      summary,
      results: this.results,
    };
    try {
      writeFileSync(temporaryPath, `${JSON.stringify(report, null, 2)}\n`, {
        encoding: "utf8",
        mode: 0o600,
        flag: "w",
      });
      chmodSync(temporaryPath, 0o600);
      renameSync(temporaryPath, path);
    } finally {
      rmSync(temporaryPath, { force: true });
    }
  }
}

function reportPath(): string {
  const raw = process.env.KODEX_E2E_DISCOVERY_REPORT;
  if (!raw) {
    throw new Error("KODEX_E2E_DISCOVERY_REPORT is required in discovery mode");
  }
  return isAbsolute(raw) ? raw : resolve(raw);
}

function safeError(raw: string | undefined): string {
  return (raw ?? "unknown error")
    .replace(
      /\b(authorization|token|password|secret|cookie)\b\s*[:=]\s*[^\s,;]+/gi,
      "$1=[REDACTED]",
    )
    .replace(/[\r\n\t]+/g, " ")
    .slice(0, 2_000);
}

export default DiscoveryReporter;
