import test from 'node:test';
import assert from 'node:assert/strict';
import { verifyDelta, verifyIdleTurns } from './queued-runtime-source-update.mjs';
test('Очередь допускается только без исполняющихся turn и supply-chain work',()=>{
  const state={at:new Date().toISOString(),runningNodes:0,claimedLeases:0,openBuilds:0,pendingAdmissions:0,pendingPromotions:0,pins:'a'.repeat(64)};
  assert.doesNotThrow(()=>verifyIdleTurns(state));
  for(const key of ['runningNodes','claimedLeases','openBuilds','pendingAdmissions','pendingPromotions'])assert.throws(()=>verifyIdleTurns({...state,[key]:1}));
  assert.throws(()=>verifyIdleTurns({...state,at:'2000-01-01T00:00:00Z'}));
});
test('SQL handover не допускает изменения runner, authority, policy или миграций',()=>{
  assert.doesNotThrow(()=>verifyDelta(['services/internal/control-plane/internal/repository/postgres/platform/sql/runtime_files_capture_entries.sql']));
  for(const path of ['libs/go/runtimecontract/contract.go','deploy/k8s/base/runner/policy.yaml','services/internal/control-plane/cmd/cli/migrations/new.sql','services/internal/control-plane/internal/app/app.go'])assert.throws(()=>verifyDelta([path]));
  assert.throws(()=>verifyDelta([]));
});
