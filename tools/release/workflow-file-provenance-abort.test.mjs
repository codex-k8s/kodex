import assert from 'node:assert/strict';
import test from 'node:test';
import {fingerprint} from './scoped-release.mjs';
import {applyWorkflowFileAbort,readWorkflowFileAbortJob} from './workflow-file-provenance-abort.mjs';
import {workflowFileAbortAck,workflowFileAbortIntent} from './workflow-file-provenance-migration-contract.mjs';

function jobFixture(){
  const name='control-plane-migrate-20261121000200',annotation='kodex.dev/cp-workflow-file-provenance-migration-plan';
  const spec={containers:[{name:'migrate',args:['up-workflow-file-provenance']}]};
  const expected={metadata:{name},spec:{template:{spec}}};
  const job={...structuredClone(expected),apiVersion:'batch/v1',kind:'Job',status:{failed:1,conditions:[{type:'Failed',status:'True'}]},
    metadata:{name,uid:'job-uid',namespace:'kodex-system',annotations:{[annotation]:'ab'.repeat(32)}}};
  const pod={metadata:{uid:'pod-uid',namespace:'kodex-system'},spec:{containers:[{name:'migrate'}]},
    status:{phase:'Failed',containerStatuses:[{name:'migrate',state:{terminated:{exitCode:1}}}]}};
  const fixture={job,pods:[pod],expected,intents:[{jobName:name}],reads:0,executions:[]};
  const reader={optionalJob:()=>{fixture.reads++;return fixture.job;},readPods:()=>fixture.pods,
    annotation,planSHA256:job.metadata.annotations[annotation],normalize:value=>value,events:[{type:'CREATE_ACK',jobUID:'job-uid'}],
    verifyExecution:(actual,pods,plan,outcome)=>{
      fixture.executions.push([actual.metadata.uid,pods[0].metadata.uid,plan.metadata.name,outcome]);
      return true;
    }};
  return {fixture,expected,executions:fixture.executions,read:(allowSuccess=false)=>readWorkflowFileAbortJob(reader,expected,fixture.intents,['CREATE_ACK'],allowSuccess)};
}

test('051 abort подтверждает отсутствие writer либо точный терминальный CHECK/DDL',()=>{
  const absent=jobFixture();absent.fixture.job=undefined;absent.fixture.intents=[];absent.fixture.pods=[];
  assert.deepEqual(absent.read(),{name:absent.expected.metadata.name,outcome:'NOT_CREATED'});
  const failed=jobFixture();assert.equal(failed.read().outcome,'FAILED');
  assert.equal(failed.executions[0][3],'failed');assert.equal(failed.fixture.reads,2);
  const check=jobFixture();
  check.fixture.job.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
  check.fixture.pods[0].status.phase='Succeeded';
  assert.equal(check.read(true).outcome,'SUCCEEDED');
  assert.throws(()=>check.read(),/ABORT_JOB_NOT_TERMINAL/);
});

test('051 abort сохраняет закрытую границу для missing, running, чужого UID и неприсоединённого Pod',()=>{
  for(const [change,code] of [
    [f=>{f.job=undefined;},/JOB_SCOPE_CHANGED/],
    [f=>{f.job.status.active=1;},/JOB_NOT_TERMINAL/],
    [f=>{f.job.status.terminating=1;},/JOB_NOT_TERMINAL/],
    [f=>{f.job.metadata.uid='other';},/JOB_SCOPE_CHANGED/],
    [f=>{f.job.spec.template.spec.containers[0].args=['up'];},/JOB_SCOPE_CHANGED/],
    [f=>{f.pods=[];},/JOB_NOT_JOINED/],
    [f=>{f.pods[0].status.reason='NodeLost';},/JOB_NOT_JOINED/],
    [f=>{f.pods[0].status.containerStatuses[0].state={running:{}};},/JOB_NOT_JOINED/],
    [f=>{f.job=undefined;f.intents=[];},/JOB_WITHOUT_INTENT/],
  ]) {
    const job=jobFixture();
    change(job.fixture);
    assert.throws(()=>job.read(),code);
  }
});

function abortFixture(stage='STOPPED'){
  let state={stage,ready:stage==='BEFORE',joined:stage==='STOPPED'};
  const proof={gooseVersion:20261121000100,sqlSHA256:'ab'.repeat(32),jobs:[]};
  const events=stage==='STOPPED'?[{type:'STOP_INTENT'}]:[],fixture={events,patches:0,proof,lostACK:false};
  const args={journal:{events,append:value=>events.push(value)},observe:()=>state,readProof:()=>fixture.proof,wait:async()=>state,
    resume:()=>{
      assert.equal(events.at(-1).type,workflowFileAbortIntent);
      fixture.patches++;
      state={stage:'RESUMED',ready:true,joined:false};
      if(fixture.lostACK)throw Error('SYNTHETIC_RESPONSE_LOST');
    }};
  return {events,proof,fixture,run:()=>applyWorkflowFileAbort(args)};
}

test('051 abort фиксируется до CAS, повтор после потерянного ACK сохраняет один запуск старого CP',async()=>{
  const f=abortFixture();f.fixture.lostACK=true;
  await assert.rejects(f.run,/SYNTHETIC_RESPONSE_LOST/);
  assert.equal(f.events.filter(e=>e.type===workflowFileAbortAck).length,0);
  const result=await f.run();
  assert.equal(result.state.stage,'RESUMED');assert.equal(f.fixture.patches,1);
  assert.equal(f.events.at(-1).abortSHA256,fingerprint(f.proof));
  await f.run();
  assert.equal(f.events.filter(e=>e.type===workflowFileAbortAck).length,1);
  assert.equal(f.fixture.patches,1);
});

test('051 abort до остановки сохраняет прежний CP, forward handoff или изменившееся proof не восстанавливаются',async()=>{
  const f=abortFixture('BEFORE');assert.equal((await f.run()).state.stage,'BEFORE');
  assert.equal(f.fixture.patches,0);
  f.fixture.proof={...f.proof,gooseVersion:20261121000200};
  await assert.rejects(f.run,/ABORT_READBACK_CHANGED/);
  const forward=abortFixture();forward.events.push({type:'PAIR_HANDOFF_INTENT'});
  await assert.rejects(forward.run,/ABORT_FORWARD_STAGE/);assert.equal(forward.fixture.patches,0);
});
