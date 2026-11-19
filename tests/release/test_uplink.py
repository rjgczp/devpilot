import io
import runpy
import json
from pathlib import Path
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'runtime/private'))
import launcher
import uplink


class UplinkTest(unittest.TestCase):
    def setUp(self):
        self.cfg = dict(mode='private-approved', target_container='c'*64, image='sha256:'+'a'*64,
                        target_ip='93.184.216.34', approval={'addresses':['93.184.216.34']},
                        routed_uplink=dict(pci_slot='0000:03:00.0', mac='02:00:00:00:00:01'))
        self.host = dict(NetworkMode='none', PidMode='', PortBindings={}, Privileged=False,
                         ReadonlyRootfs=True, Devices=[], Binds=None, CapDrop=['ALL'], CapAdd=['NET_ADMIN'],
                         SecurityOpt=['no-new-privileges:true'])
        self.container = dict(Id=self.cfg['target_container'], Image=self.cfg['image'], State={'Running':True},
                              HostConfig=self.host, Path='sleep', Args=['infinity'], Mounts=[])
        self.links = [dict(ifname='lo'), dict(ifname='uplink0', address=self.cfg['routed_uplink']['mac'], flags=['UP'])]
        self.routes = [dict(dst=dst,dev='uplink0') for dst in ('192.0.2.53', self.cfg['target_ip'])]
        self.device = b'/sys/devices/pci0000:00/0000:03:00.0/virtio0'
        self.tables = []

    def read(self, *args):
        if args[:2] == ('readlink', '-f'): return self.device
        if args[0] == 'readlink': return b'net:[222]'
        if args[:3] == ('ip', '-j', 'link'): return json.dumps(self.links).encode()
        if args[:2] in (('ip', '-4'), ('ip', '-6')):
            return json.dumps(self.routes if args[1] == '-4' else []).encode()
        if args[0] == 'cat': return b'1'
        if args == ('nft', '-j', 'list', 'tables'): return json.dumps(dict(nftables=self.tables)).encode()
        self.fail('unexpected inspection')

    def inspect(self):
        with mock.patch.object(uplink.os, 'readlink', return_value='net:[111]'):
            return uplink.inspect(self.cfg,self.container,self.read)

    def test_only_dedicated_nic_exact_routes_and_owned_firewall(self):
        self.assertFalse(self.inspect())
        self.tables.append(dict(table=dict(family='inet',name='dp_uplink')))
        self.assertTrue(self.inspect())
        self.tables.append(dict(table=dict(family='ip',name='foreign')))
        with self.assertRaises(ValueError): self.inspect()
        self.tables.clear()
        for extra in [dict(dst='default',dev='uplink0'),dict(dst='default',table=101,dev='uplink0'),
                      dict(dst=self.cfg['target_ip'],type='local',dev='lo'),
                      dict(dst='192.0.2.53',dev='control0'),dict(dst='0.0.0.0/1',dev='uplink0')]:
            with self.subTest(extra=extra):
                self.routes.append(extra)
                with self.assertRaises(ValueError): self.inspect()
                self.routes.pop()
        self.routes.pop(0)
        with self.assertRaises(ValueError): self.inspect()

    def test_host_virtual_nic_wrong_identity_and_legacy_switch_refused(self):
        for key,value in [('NetworkMode','host'),('Privileged',True),('PidMode','host'),('CapAdd',['SYS_ADMIN'])]:
            with mock.patch.dict(self.host,{key:value}), self.assertRaises(ValueError): self.inspect()
        self.device = b'/sys/devices/virtual/net/uplink0'
        with self.assertRaises(ValueError): self.inspect()
        with mock.patch.dict(self.cfg,mode='private-isolated'), self.assertRaises(ValueError): uplink.rules(self.cfg)
        with mock.patch.dict(self.cfg,target_ip='93.184.216.35'), self.assertRaises(ValueError): uplink.rules(self.cfg)
        with mock.patch.object(sys,'platform','linux'), mock.patch.object(launcher,'private_dir'), mock.patch.object(launcher,'load',return_value=self.cfg), mock.patch.object(sys,'argv',['service','run','--state','unused','--run-approved']), self.assertRaises(RuntimeError):
            launcher.main()

    def test_misbound_login_refused_before_control_pipes_or_pg(self):
        path = Path(launcher.__file__).with_name('control_child.py')
        dsn = b'postgres://devpilot_private_worker:example@postgres:5432/devpilot_private?sslmode=disable'
        for value, error in [(dsn, AssertionError), (dsn.replace(b'_worker:', b'_controller:'), RuntimeError),
                             (dsn.replace(b'@postgres:', b'@other:'), RuntimeError),
                             (dsn + b'&host=other', RuntimeError)]:
            with mock.patch.object(sys,'argv',['control_child','worker','inc','private-approved','--bound-login']), \
                 mock.patch('os.getuid',return_value=65532), mock.patch.object(Path,'open',return_value=io.BytesIO(value)), \
                 mock.patch('os.pipe',side_effect=AssertionError('past role binding')), self.assertRaises(error):
                runpy.run_path(str(path))
