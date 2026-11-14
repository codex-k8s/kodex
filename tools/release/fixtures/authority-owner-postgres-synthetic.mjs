import {canonicalPostgresBackupSpec} from '../backup-postgres-boundary.mjs';

// Только transport fixture: реальные canonical Pod/STS формы не становятся
// результатом SQL либо разрешением ротации; owner query задаётся отдельно.
export function authorityOwnerPostgresObjects({podUID='10000000-0000-4000-8000-000000000071',ownerUID='10000000-0000-4000-8000-000000000070'}={}){
 const spec=canonicalPostgresBackupSpec();
 return {set:{apiVersion:'apps/v1',kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:ownerUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'}},spec:{template:{spec}}},
  pod:{apiVersion:'v1',kind:'Pod',metadata:{name:'kodex-postgresql-0',namespace:'kodex-system',uid:podUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'},
   ownerReferences:[{apiVersion:'apps/v1',kind:'StatefulSet',name:'kodex-postgresql',uid:ownerUID,controller:true}]},
   spec:{...structuredClone(spec),volumes:[...spec.volumes,{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]},
   status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'postgresql',ready:true,state:{running:{}},imageID:spec.containers[0].image}]}},
  backend:{systemIdentifier:'18446744073709551615',databaseOID:'20000',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null}};
}
