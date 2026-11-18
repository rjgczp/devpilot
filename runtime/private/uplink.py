"""Dedicated, pre-provisioned Linux NIC behind the existing two Permit filters."""
import hashlib
import ipaddress
import json
import os
import re
import stat
from pathlib import Path


def binding(config):
    spec = config['routed_uplink']
    if (config['mode'] != 'private-approved' or set(spec) != {'pci_slot', 'mac'}
            or not re.fullmatch(r'[0-9a-f]{4}:[0-9a-f]{2}:[0-1][0-9a-f]\.[0-7]', spec['pci_slot'])
            or not re.fullmatch(r'(?:[0-9a-f]{2}:){5}[0-9a-f]{2}', spec['mac'])):
        raise ValueError('dedicated PCI uplink binding required')
    target = ipaddress.ip_address(config['target_ip'])
    if not target.is_global or target.is_multicast or str(target) not in config['approval']['addresses']:
        raise ValueError('approved literal required')
    return target


def check_public_pins(config, state):
    for file, key in ((state / 'public.pem', 'supervisor_key_sha256'),
                      (Path(config['ca_file']), 'target_ca_sha256')):
        info = file.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or info.st_mode & 0o022 or not info.st_mode & 0o004 or info.st_size > 2**20):
            raise ValueError('operator-owned public material required')
        if hashlib.sha256(file.read_bytes()).hexdigest() != config[key]:
            raise ValueError('public key or target CA binding mismatch')
    return True


def rules(config):
    target = binding(config)
    family = 'ip' if target.version == 4 else 'ip6'
    # The real recursive resolver owns the existing Probe wire address on the
    # dedicated network. The holder performs no resolution, DNAT or TLS proxying.
    return f'''table inet dp_uplink {{
 chain input {{ type filter hook input priority filter; policy drop; }}
 chain output {{ type filter hook output priority filter; policy drop; }}
 chain forward {{ type filter hook forward priority filter; policy drop;
  iifname "dp*" oifname "uplink0" ip daddr 192.0.2.53 tcp dport 53 accept
  iifname "dp*" oifname "uplink0" {family} daddr {target} tcp dport 443 accept
  iifname "uplink0" oifname "dp*" ct state established ip saddr 192.0.2.53 tcp sport 53 accept
  iifname "uplink0" oifname "dp*" ct state established {family} saddr {target} tcp sport 443 accept
 }}
 chain source {{ type nat hook postrouting priority srcnat; policy accept;
  iifname "dp*" oifname "uplink0" masquerade
 }}
}}
'''


def inspect(config, container, read):
    """Read only, before any downstream link exists. read executes inside the holder."""
    target = binding(config)
    hw = config['routed_uplink']
    host = container['HostConfig']
    if (container['Id'] != config['target_container'] or container['Image'] != config['image']
            or not container['State']['Running'] or host['NetworkMode'] != 'none'
            or host['Privileged'] or host['PortBindings'] or host['PidMode'] or host['Devices']
            or host['Binds'] or any(m.get('Type') != 'tmpfs' or m.get('Destination') != '/var/lib/postgresql' for m in container['Mounts'])
            or not host['ReadonlyRootfs'] or host['CapDrop'] != ['ALL']
            or host['CapAdd'] != ['NET_ADMIN'] or 'no-new-privileges:true' not in (host['SecurityOpt'] or [])
            or container['Path'] != 'sleep' or container['Args'] != ['infinity']):
        raise ValueError('dedicated holder required')
    netns = read('readlink', '/proc/self/ns/net').decode().strip()
    if not re.fullmatch(r'net:\[\d+\]', netns) or netns == os.readlink('/proc/self/ns/net'):
        raise ValueError('host network namespace')
    links = json.loads(read('ip', '-j', 'link'))
    if {x['ifname'] for x in links} != {'lo', 'uplink0'}:
        raise ValueError('unexpected uplink interface')
    nic = next(x for x in links if x['ifname'] == 'uplink0')
    device = read('readlink', '-f', '/sys/class/net/uplink0/device').decode().strip().split('/')
    physical = device[-1] == hw['pci_slot'] or (
        len(device) >= 2 and device[-2] == hw['pci_slot'] and re.fullmatch(r'virtio\d+', device[-1]))
    if (device[:3] != ['', 'sys', 'devices'] or not physical or nic['address'] != hw['mac'] or 'UP' not in nic['flags'] or 'master' in nic):
        raise ValueError('dedicated NIC identity mismatch')
    expected = {'192.0.2.53', str(target)}
    found = set()
    for family in ('-4', '-6'):
        routes = json.loads(read('ip', family, '-j', 'route', 'show', 'table', 'all'))
        for r in routes:
            dst = r.get('dst', 'default')
            kind = r.get('type', 'unicast')
            if dst in ('default', '0.0.0.0/0', '::/0'):
                raise ValueError('default upstream route')
            if kind in ('local', 'broadcast', 'multicast') and dst not in expected:
                continue
            if (dst not in expected or kind != 'unicast' or r.get('dev') != 'uplink0'
                    or r.get('table', 'main') != 'main' or 'nexthops' in r or 'encap' in r
                    or r.get('gateway') in expected):
                raise ValueError('only exact upstream host routes allowed')
            found.add(dst)
    if found != expected:
        raise ValueError('missing DNS or HTTPS host route')
    for path in ('/proc/sys/net/ipv4/ip_forward', '/proc/sys/net/ipv6/conf/all/forwarding'):
        if read('cat', path).strip() != b'1':
            raise ValueError('dedicated namespace forwarding disabled')
    tables = json.loads(read('nft', '-j', 'list', 'tables'))['nftables']
    existing = [row['table'] for row in tables if 'table' in row]
    if any(t['family'] != 'inet' or t['name'] != 'dp_uplink' for t in existing):
        raise ValueError('unexpected firewall table')
    return bool(existing)
