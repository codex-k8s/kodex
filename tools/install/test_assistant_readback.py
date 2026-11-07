"""Проверяет настоящий CLI, loopback HTTP и завершение дочернего port-forward."""
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent
UID = '11111111-1111-4111-8111-111111111111'
KUBECTL = r'''#!/usr/bin/env python3
import http.server,json,os,pathlib,signal,sys,time
root=pathlib.Path(os.environ['HOME']);scenario=os.environ['SCENARIO'];args=sys.argv[1:]
if 'port-forward'in args:
    (root/'child-pid').write_text(str(os.getpid()))
    assert args[-2:]==['--address','127.0.0.1'] and 'pod/synthetic-controller'in args
    if scenario=='no-forward':raise SystemExit(3)
    if scenario=='ignores-term':signal.signal(signal.SIGTERM,signal.SIG_IGN)
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            assert self.path=='/assistant/readyz'
            if scenario=='cancel':
                (root/'request-started').write_text('yes');time.sleep(30)
            status=302 if scenario=='redirect'else 503 if scenario=='not-ready'else 204
            self.send_response(status)
            if status==302:self.send_header('Location','http://example.invalid/never-follow')
            self.end_headers()
            if status!=204:self.wfile.write(b'synthetic-sensitive-upstream-response')
        def log_message(self,*arguments):pass
    server=http.server.HTTPServer(('127.0.0.1',0),Handler)
    print('Forwarding from 127.0.0.1:'+str(server.server_port)+' -> 9090',flush=True)
    server.serve_forever()
elif 'pod'in args:
    print(json.dumps({'metadata':{'name':'synthetic-controller','namespace':'kodex-system',
        'uid':os.environ['EXPECTED_UID'],'labels':{'app.kubernetes.io/name':'foreign'if scenario=='foreign-pod'else'runtime-controller'}},
        'status':{'conditions':[{'type':'Ready','status':'True'}]}}))
elif 'lease'in args:
    count=root/'lease-reads';value=int(count.read_text())+1 if count.exists()else 1;count.write_text(str(value))
    holder='foreign'if scenario=='leader-changed'and value>1 else os.environ['EXPECTED_UID']
    print(json.dumps({'spec':{'holderIdentity':holder}}))
else:raise SystemExit(23)
'''


class Tests(unittest.TestCase):
    def test_sigterm_during_http_read_joins_the_owned_forwarder(self):
        with tempfile.TemporaryDirectory()as directory:
            root=Path(directory);binary=root/'kubectl';binary.write_text(KUBECTL);binary.chmod(0o700)
            environment={'PATH':directory+':'+os.environ['PATH'],'HOME':directory,'LANG':'C',
                         'SCENARIO':'cancel','EXPECTED_UID':UID}
            process=subprocess.Popen(['python3',str(ROOT/'assistant_readback.py'),'--context','synthetic',
                '--pod','synthetic-controller','--pod-uid',UID],env=environment,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            try:
                deadline=time.monotonic()+5
                while not(root/'request-started').exists()and time.monotonic()<deadline:time.sleep(.05)
                self.assertTrue((root/'request-started').exists())
                process.terminate();process.communicate(timeout=6)
                self.assertNotEqual(process.returncode,0)
                with self.assertRaises(ProcessLookupError):os.kill(int((root/'child-pid').read_text()),0)
            finally:
                if process.poll()is None:process.kill();process.communicate(timeout=6)

    def test_cli_requires_exact_leader_ready_response_and_always_joins_forwarder(self):
        for scenario in ('ready','not-ready','redirect','leader-changed','foreign-pod','no-forward','ignores-term'):
            with self.subTest(scenario=scenario),tempfile.TemporaryDirectory()as directory:
                root=Path(directory);binary=root/'kubectl';binary.write_text(KUBECTL);binary.chmod(0o700)
                environment={'PATH':directory+':'+os.environ['PATH'],'HOME':directory,'LANG':'C',
                             'SCENARIO':scenario,'EXPECTED_UID':UID,
                             'HTTP_PROXY':'http://example.invalid:1234','HTTPS_PROXY':'http://example.invalid:1234'}
                result=subprocess.run(['python3',str(ROOT/'assistant_readback.py'),'--context','synthetic',
                    '--pod','synthetic-controller','--pod-uid',UID],env=environment,capture_output=True,text=True,timeout=25)
                self.assertEqual(result.returncode==0,scenario in ('ready','ignores-term'),result.stderr)
                self.assertNotIn('synthetic-sensitive-upstream-response',result.stdout+result.stderr)
                self.assertNotIn('example.invalid',result.stdout+result.stderr)
                pid=root/'child-pid'
                if pid.exists():
                    with self.assertRaises(ProcessLookupError):os.kill(int(pid.read_text()),0)
                else:self.assertEqual(scenario,'foreign-pod')
                if scenario in ('ready','leader-changed','ignores-term'):
                    self.assertEqual((root/'lease-reads').read_text(),'2')

    def test_invalid_pod_or_uid_never_starts_kubectl(self):
        for pod,uid in [('synthetic/other',UID),('synthetic','bad-uid')]:
            result=subprocess.run(['python3',str(ROOT/'assistant_readback.py'),'--context','synthetic',
                '--pod',pod,'--pod-uid',uid],env={'PATH':'/usr/bin:/bin'},capture_output=True,text=True,timeout=5)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(result.stderr.strip(),'System assistant exact leader readback failed')


if __name__=='__main__':unittest.main()
