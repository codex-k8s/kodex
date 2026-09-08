import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {policyDigest} from './runner-policy-model.mjs';
import {validateFuturePolicy,validateFutureJobProof} from './authority-freshness-job-proof.mjs';
const image='registry.invalid/authority@sha256:'+'a'.repeat(64);
function policy(){const p={metadata:{name:'versioned-policy',labels:{'kodex.dev/owner-intent':'true'}},immutable:true,data:{authorityImage:image.replace('a'.repeat(64),'b'.repeat(64)),authorityIssuerImage:image,policyRevision:'revision'}};p.data.policySHA256=policyDigest(p.data);return p;}
const binding=()=>({spec:{paramRef:{name:'versioned-policy',namespace:'kodex-system',parameterNotFoundAction:'Deny'},validationActions:['Deny']}});
test('future policy must bind exact immutable payload, issuer image and deny admission',()=>{
 const p=policy(),parameters={spec:structuredClone(p.data)};validateFuturePolicy(p,parameters,binding());
 for(const mutate of [p=>p.immutable=false,p=>p.data.authorityIssuerImage='registry.invalid/latest',p=>p.data.policyRevision='foreign']){const next=structuredClone(p);mutate(next);assert.throws(()=>validateFuturePolicy(next,parameters,binding()));}
 const b=binding();b.spec.validationActions=['Warn'];assert.throws(()=>validateFuturePolicy(p,parameters,b));
 assert.throws(()=>validateFuturePolicy(p,{spec:{...p.data,authorityIssuerImage:p.data.authorityImage}},binding()));
});
test('proof is exact source, executable, image, owner Job and completed effect',()=>{
 const p=policy(),cap={revision:'c'.repeat(40),imageBinaries:{issuer:'d'.repeat(64)}};
 const job={metadata:{name:'owner-job',uid:'job-uid',labels:{'kodex.dev/image-admission-phase':'promote'},annotations:{'kodex.dev/admission-policy-revision':'revision'}},spec:{template:{spec:{containers:[{name:'job',image}]}}},status:{succeeded:1}};
 const proof={version:1,namespaceUID:'namespace',revision:cap.revision,binarySHA256:cap.imageBinaries.issuer,job:'owner-job',jobUID:'job-uid',jobSpecSHA256:fingerprint(job.spec),policySHA256:p.data.policySHA256,workload:'image-promotion',image,imageID:image};
 validateFutureJobProof(proof,job,p,cap,'namespace');
 for(const mutate of [p=>p.revision='e'.repeat(40),p=>p.binarySHA256='e'.repeat(64),p=>p.imageID=undefined,p=>p.imageID=image+'suffix',p=>p.jobUID='foreign',p=>p.workload='image-admission']){const next=structuredClone(proof);mutate(next);assert.throws(()=>validateFutureJobProof(next,job,p,cap,'namespace'));}
 for(const mutate of [j=>j.status.succeeded=0,j=>j.spec.template.spec.containers[0].image='foreign',j=>j.metadata.annotations['kodex.dev/admission-policy-revision']='old']){const next=structuredClone(job);mutate(next);assert.throws(()=>validateFutureJobProof(proof,next,p,cap,'namespace'));}
});
