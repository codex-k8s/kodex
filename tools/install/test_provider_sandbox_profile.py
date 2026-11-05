"""Runtime sandbox — отдельная bounded проекция, без изменения image admission и исторических Jobs."""
import copy
import unittest
import provider_sandbox_profile as profile
from deployment_profile import Rejected


def fixture():
    labels={'app.kubernetes.io/part-of':'kodex'}
    data={'nodeReadbackImage':'images.example.invalid/kodex/runner@sha256:'+'a'*64,'roleRuntimeContractRevision':'3','providerAppArmorProfile':'',
          'policyRevision':'1','orchestrationRevision':'b'*40,'requiredTools':'synthetic'}
    data['policySHA256']=profile.digest({k:v for k,v in data.items()if k!='orchestrationRevision'})
    return [dict(apiVersion='v1',kind='ConfigMap',metadata=dict(name=profile.SOURCE,namespace='kodex-system',labels=copy.deepcopy(labels)),immutable=True,data=data),
      dict(apiVersion='apps/v1',kind='Deployment',metadata=dict(name='runtime-controller',namespace='kodex-system',labels=copy.deepcopy(labels)),
        spec=dict(replicas=2,template=dict(metadata=dict(annotations={'synthetic':'unchanged'}),spec=dict(containers=[dict(name='runtime-controller',image='images.example.invalid/controller@sha256:'+'c'*64,env=[dict(name=profile.ENV,valueFrom=dict(configMapKeyRef=dict(name=profile.SOURCE,key='providerAppArmorProfile'))),dict(name='UNCHANGED',value='true')]),dict(name='guard',image='images.example.invalid/guard@sha256:'+'d'*64)])))),
      dict(apiVersion='admissionregistration.k8s.io/v1',kind='ValidatingAdmissionPolicyBinding',metadata=dict(name=profile.BINDING,labels=copy.deepcopy(labels)),
        spec=dict(policyName=profile.BINDING,validationActions=['Deny'],paramRef=dict(name=profile.SOURCE,namespace='kodex-system',parameterNotFoundAction='Deny'),matchResources=dict(namespaceSelector=dict(matchLabels={'kubernetes.io/metadata.name':'kodex-runtime'})))),
      dict(apiVersion='batch/v1',kind='Job',metadata=dict(name='synthetic-completed',namespace='kodex-system',labels=copy.deepcopy(labels)),spec=dict(template=dict(spec=dict(containers=[dict(name='history',image='images.example.invalid/job@sha256:'+'e'*64)]))))]


class Tests(unittest.TestCase):
    def test_default_is_unchanged_for_either_engine(self):
        for engine in ('codex','opencode'):
            values=fixture();values[0]['data']['nodeReadbackImage']=values[0]['data']['nodeReadbackImage'].replace('/runner@','/'+engine+'@')
            data=values[0]['data'];data['policySHA256']=profile.digest({k:v for k,v in data.items()if k not in ('orchestrationRevision','policySHA256')})
            self.assertEqual(profile.transform(values),values)
            actual=profile.transform(values,profile.PROFILE)
            self.assertEqual(actual[0],values[0]);self.assertEqual(actual[3],values[3])
            self.assertEqual(profile.active(actual),profile.PROFILE)

    def test_exact_delta_idempotence_and_disabled_forward_return(self):
        values=fixture();before=copy.deepcopy(values);actual=profile.transform(values,profile.PROFILE)
        self.assertEqual(values,before);self.assertEqual(profile.transform(actual,profile.PROFILE),actual)
        touched=profile.transition(before,actual);self.assertEqual([r['kind']for r in touched],['ConfigMap','ValidatingAdmissionPolicyBinding','Deployment'])
        self.assertEqual(set(actual[-1]['data']),set(profile.FIELDS));self.assertTrue(actual[-1]['immutable'])
        self.assertEqual(actual[1]['spec']['template']['spec']['containers'][1],before[1]['spec']['template']['spec']['containers'][1])
        disabled=profile.transform(actual,'');self.assertEqual(profile.active(disabled),'');self.assertEqual(len(disabled),len(actual))
        self.assertEqual(disabled[0],before[0]);self.assertEqual(disabled[3],before[3]);profile.transition(actual,disabled)

    def test_reject_source_owner_namespace_digest_and_unpinned_image(self):
        for change in ('owner','namespace','immutable','digest','image','revision','duplicate'):
            values=fixture();source=values[0]
            if change=='owner':source['metadata']['labels']={}
            elif change=='namespace':source['metadata']['namespace']='foreign'
            elif change=='immutable':source['immutable']=False
            elif change=='digest':source['data']['policySHA256']='f'*64
            elif change=='image':source['data']['nodeReadbackImage']='images.example.invalid/runner:latest'
            elif change=='revision':source['data']['roleRuntimeContractRevision']='0'
            else:values.append(copy.deepcopy(source))
            if change in ('image','revision'):source['data']['policySHA256']=profile.digest({k:v for k,v in source['data'].items()if k not in ('policySHA256','orchestrationRevision')})
            with self.subTest(change=change),self.assertRaises(Rejected):profile.transform(values,profile.PROFILE)

    def test_reject_unknown_profile_and_binding_env_weakening(self):
        for bad in ('owner-profile','Unconfined','RuntimeDefault',True,{}):
            with self.assertRaises(Rejected):profile.transform(fixture(),bad)
        for change in ('allow','namespace','env','key','literal','duplicate'):
            values=fixture();binding=values[2];container=values[1]['spec']['template']['spec']['containers'][0]
            if change=='allow':binding['spec']['validationActions']=['Warn']
            elif change=='namespace':binding['spec']['paramRef']['namespace']='foreign'
            elif change=='env':container['env'][0]['valueFrom']['configMapKeyRef']['name']='foreign'
            elif change=='key':container['env'][0]['valueFrom']['configMapKeyRef']['key']='arbitrary'
            elif change=='literal':container['env'][0]={'name':profile.ENV,'value':profile.PROFILE}
            else:container['env'].append(copy.deepcopy(container['env'][0]))
            with self.subTest(change=change),self.assertRaises(Rejected):profile.transform(values,profile.PROFILE)

    def test_reject_projection_unknown_fields_wrong_anchor_and_stale_markers(self):
        for change in ('data','anchor','source','name','marker','duplicate','unbind'):
            values=profile.transform(fixture(),profile.PROFILE);projection=values[-1]
            if change=='data':projection['data']['untrusted']='true'
            elif change=='anchor':projection['data']['nodeReadbackImage']='images.example.invalid/foreign@sha256:'+'f'*64
            elif change=='source':projection['metadata']['annotations'][profile.SOURCE_DIGEST]='f'*64
            elif change=='name':projection['metadata']['name']+='foreign'
            elif change=='marker':values[1]['spec']['template']['metadata']['annotations'][profile.DIGEST]='f'*64
            elif change=='duplicate':values.append(copy.deepcopy(projection))
            else:values[2]['spec']['paramRef']['name']=profile.SOURCE
            with self.subTest(change=change),self.assertRaises(Rejected):profile.active(values)

    def test_transition_refuses_changes_outside_runtime_projection(self):
        for change in ('job','image','sidecar','replicas','policy'):
            values=fixture();actual=profile.transform(values,profile.PROFILE)
            if change=='job':actual[3]['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/other:latest'
            elif change=='image':actual[1]['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/controller:latest'
            elif change=='sidecar':actual[1]['spec']['template']['spec']['containers'][1]['env']=[dict(name='UNTRUSTED',value='true')]
            elif change=='replicas':actual[1]['spec']['replicas']=0
            else:actual[0]['data']['requiredTools']='changed'
            with self.subTest(change=change),self.assertRaises(Rejected):profile.transition(values,actual)

    def test_registry_rebase_preserves_profile_and_new_content_bound_identity(self):
        previous=profile.transform(fixture(),profile.PROFILE);updated=copy.deepcopy(previous)
        data=updated[0]['data'];data['nodeReadbackImage']='internal.example.invalid/runner@sha256:'+'a'*64
        data['policySHA256']=profile.digest({k:v for k,v in data.items()if k not in ('policySHA256','orchestrationRevision')})
        with self.assertRaises(Rejected):profile.active(updated)
        current=profile.rebase(updated,previous)
        self.assertEqual(profile.active(current),profile.PROFILE)
        self.assertNotEqual(current[-1]['metadata']['name'],previous[-1]['metadata']['name'])
        self.assertEqual(current[-1]['data']['nodeReadbackImage'],data['nodeReadbackImage'])
        self.assertEqual(current[0],updated[0]);self.assertEqual(current[3],updated[3])


if __name__=='__main__':unittest.main()
