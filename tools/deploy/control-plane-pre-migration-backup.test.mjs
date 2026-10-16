import test from "node:test";
import assert from "node:assert/strict";
import { sourceDatabasePod } from "./control-plane-pre-migration-backup.mjs";

test("pre-migration backup pins singleton database and exact ready instance",()=>{
 const stateful={kind:"StatefulSet",metadata:{name:"kodex-postgresql",namespace:"kodex-system",uid:"database"},spec:{replicas:1}};
 const pod={metadata:{name:"database-0",uid:"pod",ownerReferences:[{kind:"StatefulSet",controller:true,uid:"database"}]},
  status:{phase:"Running",containerStatuses:[{name:"postgresql",ready:true,containerID:"container",imageID:"image",restartCount:0}]}};
 assert.equal(sourceDatabasePod(stateful,[pod]).podUID,"pod");
 for(const change of [value=>value.metadata.namespace="foreign",value=>value.spec.replicas=2,value=>value.metadata.uid="replacement"]){
  const value=structuredClone(stateful);change(value);assert.throws(()=>sourceDatabasePod(value,[pod]));
 }
 assert.throws(()=>sourceDatabasePod(stateful,[pod,pod]));
 pod.status.containerStatuses[0].ready=false;assert.throws(()=>sourceDatabasePod(stateful,[pod]));
});
