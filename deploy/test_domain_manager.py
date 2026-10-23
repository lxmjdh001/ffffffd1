import importlib.util
from pathlib import Path
import tempfile
import json
import subprocess
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('domains',Path(__file__).with_name('domain-manager.py'))
domains=importlib.util.module_from_spec(spec);spec.loader.exec_module(domains)

class TestPublicVerification(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.root=patch.object(domains,'ROOT',Path(self.temp.name))
        self.root.start()
    def tearDown(self):
        self.root.stop();self.temp.cleanup()
    def connection(self,status=200,location=None):
        class Connection:
            def __init__(self,*args,**kwargs):pass
            def request(self,method,path,headers):self.path=path
            def getresponse(self):return self
            def read(self,count):
                if status==200:return (domains.ROOT/self.path.lstrip('/')).read_bytes()
                return b'404 page not found'
            def getheader(self,name):return location
            def close(self):pass
        Connection.status=status
        return Connection
    def test_random_platform_marker_is_required(self):
        with patch.object(domains,'PublicConnection',self.connection()):
            domains.verify_http('proxy.example.com','a'*48)
        self.assertEqual(list(domains.ROOT.glob('.well-known/acme-challenge/*')),[])
    def test_404_is_not_verification(self):
        with patch.object(domains,'PublicConnection',self.connection(404)):
            with self.assertRaisesRegex(ValueError,'HTTP 404'):
                domains.verify_http('proxy.example.com','a'*48)
    def test_redirect_to_another_host_is_rejected(self):
        with patch.object(domains,'PublicConnection',self.connection(302,'http://other.example.com/token')):
            with self.assertRaisesRegex(ValueError,'其他站点'):
                domains.verify_http('proxy.example.com','a'*48)
    def test_private_dns_result_is_not_contacted(self):
        with patch.object(domains.socket,'getaddrinfo',return_value=[(None,None,None,None,('127.0.0.1',80))]), patch.object(domains.socket,'create_connection') as connect:
            with self.assertRaisesRegex(ValueError,'公网'):
                domains.PublicConnection('proxy.example.com',80).connect()
            connect.assert_not_called()
    def test_https_config_serves_same_host_verification_path(self):
        self.assertEqual(domains.config('proxy.example.com',True).count('location ^~ /.well-known/acme-challenge/'),2)
    def worker(self, cert_fails):
        events=[]
        row={'id':'a'*32,'hostname':'proxy.example.com','token':'b'*48,'verification_method':'http','ssl_status':'verifying','ssl_retry_at':None}
        def sql(query):
            if query.startswith('SELECT coalesce'):return json.dumps([row])
            if query.startswith('SELECT count'):return '1'
            events.append(query)
            return ''
        def run(args,**kwargs):
            if args[0]=='/bin/bash':
                events.append('certbot')
                if cert_fails:raise subprocess.CalledProcessError(1,args,stderr='HTTP-01 failed')
            return ''
        with patch.object(domains,'CONF',Path(self.temp.name)/'conf'), patch.object(domains,'sql',side_effect=sql), patch.object(domains,'run',side_effect=run), patch.object(domains,'verify_http',side_effect=lambda *a,**k:events.append('route-proof')), patch('builtins.print'):
            domains.main()
        return events
    def test_acme_failure_does_not_mark_binding_verified(self):
        events=self.worker(True)
        self.assertNotIn('route-proof',events)
        self.assertFalse(any("ssl_status='ready'" in event for event in events))
        self.assertTrue(any("ssl_status='error'" in event for event in events))
    def test_ready_requires_both_acme_and_public_route(self):
        events=self.worker(False)
        ready=next(i for i,event in enumerate(events) if "ssl_status='ready'" in event)
        self.assertLess(events.index('certbot'),events.index('route-proof'))
        self.assertLess(events.index('route-proof'),ready)
        self.assertIn("verified_at=CASE",events[ready])

if __name__=='__main__':unittest.main()
