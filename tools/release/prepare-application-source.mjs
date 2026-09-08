#!/usr/bin/env node

import { prepareApplicationSource } from "./application-source.mjs";

try {
  const args = process.argv.slice(2);
  if (args.length !== 6 || args[0] !== "--source" || args[2] !== "--revision" ||
    args[4] !== "--confirm" || args[5] !== "PREPARE-STAGING-SOURCE") throw new Error("INVALID_SOURCE_PREPARATION_ARGUMENTS");
  process.stdout.write(`${JSON.stringify(prepareApplicationSource({ path: args[1], revision: args[3] }))}\n`);
} catch (error) {
  const code = /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "SOURCE_PREPARATION_FAILED";
  process.stderr.write(`Application source preparation failed: ${code}\n`);
  process.exitCode = 1;
}
