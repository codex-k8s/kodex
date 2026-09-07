#!/usr/bin/env python3
"""Запускает реальные readback/affinity scripts с синтетическим transport, без API/PG."""
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
STUB = r'''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
data=json.loads(Path(os.environ['DISCOVERY_FIXTURE']).read_text())
labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/local-profile':'hot-reload','kodex.dev/profile':'web-only'}
if args==['config','current-context']: print('fixture-local')
elif '--raw=/readyz' in args: print('ok')
elif 'namespace/kodex-system' in args:
 print(json.dumps({'metadata':{'name':'kodex-system','labels':labels}}))
elif 'pod/kodex-postgresql-0' in args:
 print(json.dumps({'metadata':{'namespace':'kodex-system','labels':labels},'status':{'phase':'Running','containerStatuses':[{'name':'postgresql','ready':True}]}}))
elif 'psql' in args:
 sql=sys.stdin.read()
 assert 'READ ONLY' in sql and 'statement_timeout' in sql
 if "'run_accounts'" in sql:
  assert "jsonb_array_elements_text(:'selected_account_refs'::jsonb)" in sql
  requested=json.loads(next(value.split('=',1)[1] for value in args if value.startswith('selected_account_refs=')))
  result=dict(data['readback'])
  result['selected_accounts']={item['ref']:item['stable_key'] for item in data['active'] if item['ref'] in requested}
  print(json.dumps(result))
 else:
  assert 'runtime_boundary_consistent' in sql
  for row in data['affinity']: print(json.dumps(row))
else: sys.exit(2)
'''


def fixture():
    refs = {name: 'run_'+value for name,value in {
        'firstRunRef':'first0001','continuationRunRef':'continue1','workflowRunRef':'workflow1',
        'scheduledRunRef':'schedule1','instructionRunRef':'instruct1'}.items()}
    refs.update(publishedInstructionRef='instr_published1', coordinatorProviderAccountRef='pacc_first0001', analystProviderAccountRef='pacc_second001')
    mapping={ref:refs['analystProviderAccountRef'] if name=='scheduledRunRef' else refs['coordinatorProviderAccountRef']
             for name,ref in refs.items() if name.endswith('RunRef')}
    rows=[dict(run_ref=ref,found=True,session_ref='ses_shared001' if ref in (refs['firstRunRef'],refs['continuationRunRef']) else 'ses_'+ref,
               session_account_ref=account,runtime_revision_count=1,runtime_boundary_consistent=True,runtime_account_refs=[account])
          for ref,account in mapping.items()]
    return dict(state={'version':1,'refs':refs},readback={'run_accounts':mapping,'instruction_runtime_count':1,'instruction_runtime_valid':True},
                active=[{'ref':'pacc_first0001','stable_key':'default-openai-codex'},
                        {'ref':'pacc_second001','stable_key':'account-'+'a'*32}],affinity=rows)


def main():
    cases=['generated-keys','third-active','invalid-ref','missing-ref','same-account','missing-active',
           'wrong-affinity','wrong-runtime-account','wrong-session','missing-run','instruction-invalid']
    with tempfile.TemporaryDirectory(prefix='kodex-discovery-fixture-') as temporary:
        private=Path(temporary)
        (private/'kubectl').write_text(STUB)
        (private/'kubectl').chmod(0o700)
        kube=private/'kubeconfig';kube.write_text('synthetic');kube.chmod(0o600)
        for case in cases:
            data=copy.deepcopy(fixture())
            refs=data['state']['refs']
            if case=='third-active':data['active'].append({'ref':'pacc_third0001','stable_key':'account-'+'b'*32})
            elif case=='invalid-ref':refs['coordinatorProviderAccountRef']='invalid/ref'
            elif case=='missing-ref':del refs['analystProviderAccountRef']
            elif case=='same-account':refs['analystProviderAccountRef']=refs['coordinatorProviderAccountRef']
            elif case=='missing-active':data['active'].pop()
            elif case=='wrong-affinity':data['readback']['run_accounts'][refs['firstRunRef']]=refs['analystProviderAccountRef']
            elif case=='wrong-runtime-account':data['affinity'][0]['runtime_account_refs']=[refs['analystProviderAccountRef']]
            elif case=='wrong-session':data['affinity'][1]['session_ref']='ses_foreign001'
            elif case=='missing-run':del data['readback']['run_accounts'][refs['firstRunRef']]
            elif case=='instruction-invalid':data['readback']['instruction_runtime_valid']=False
            state=private/'state.json';state.write_text(json.dumps(data['state']));state.chmod(0o600)
            transport=private/'transport.json';transport.write_text(json.dumps(data));transport.chmod(0o600)
            env={**os.environ,'PATH':str(private)+os.pathsep+os.environ['PATH'],'DISCOVERY_FIXTURE':str(transport)}
            result=subprocess.run(['bash',str(ROOT/'tools/dev/verify-discovery-readback.sh'),'--context','fixture-local',
                                   '--kubeconfig',str(kube),'--state',str(state)],env=env,capture_output=True,timeout=20)
            expected=case in ('generated-keys','third-active')
            assert (result.returncode==0)==expected, 'discovery fixture outcome mismatch: '+case
    print('discovery provider fixtures: 11 scenarios passed')


if __name__=='__main__':main()
