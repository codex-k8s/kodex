#!/usr/bin/env node

import { createApplicationSource } from "./application-source.mjs";

try {
  const args = process.argv.slice(2);
  if (args.length !== 8 || args[0] !== "--repository" || args[2] !== "--source" || args[4] !== "--revision" ||
    args[6] !== "--confirm" || args[7] !== "CREATE-STAGING-SOURCE") throw new Error("INVALID_SOURCE_CREATION_ARGUMENTS");
  process.stdout.write(`${JSON.stringify(createApplicationSource(args[1], { path: args[3], revision: args[5] }))}\n`);
} catch (error) {
  const code = /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "SOURCE_CREATION_FAILED";
  process.stderr.write(`Application source creation failed: ${code}\n`);
  process.exitCode = 1;
}
