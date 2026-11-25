"""Veth-scoped trusted-host filter; never installed by Probe or Worker.

Two nft tables positively attest each packet across hooks. Either table alone
rejects traffic. Host root/kernel and simultaneous removal of both are trusted.
"""
import datetime
import hashlib
import ipaddress
import json
import re
import threading


def public4(value):
    ip = ipaddress.ip_address(value)
    if ip.version != 4 or not ip.is_global or ip.is_multicast or ip.is_reserved:
        raise ValueError('ordinary IPv4 required')
    return str(ip)


def stable(value):
    if isinstance(value, dict):
        return {k: stable(v) for k, v in value.items()
                if k not in ('handle', 'packets', 'bytes', 'metainfo', 'elem')}
    if isinstance(value, list):
        return [stable(v) for v in value if not isinstance(v, dict) or 'metainfo' not in v]
    return value


class HostFilter:
    def __init__(self, run, identity, interface, source, resolver, target):
        if not re.fullmatch(r'[a-zA-Z0-9_-]{1,15}', interface):
            raise ValueError('owned veth required')
        self.run, self.interface = run, interface
        self.source = str(ipaddress.IPv4Address(source))
        self.resolver, self.target = public4(resolver), public4(target)
        if self.resolver == self.target:
            raise ValueError('DNS exception cannot be an HTTPS target')
        suffix = hashlib.sha256(identity.encode()).hexdigest()[:12]
        self.tables = ['dpl_policy_' + suffix, 'dpl_guard_' + suffix]
        self.hashes = None
        self.lock = threading.RLock()
        self.closed = False

    def script(self):
        interface, source = self.interface, self.source
        match = f'iifname "{interface}"'
        reverse = f'oifname "{interface}"'
        first, second = self.tables
        sets = 'set allow { type ipv4_addr . inet_service; flags timeout; }'
        local = f'chain input {{ type filter hook input priority 50; policy accept; iifname "{interface}" counter drop; }}\nchain output {{ type filter hook output priority 50; policy accept; oifname "{interface}" counter drop; }}'
        return f'''table inet {first} {{
 {sets}
 {local}
 chain forward {{ type filter hook forward priority 50; policy accept;
  {match} jump outbound
  {reverse} jump inbound
 }}
 chain outbound {{
  meta mark set 0
  ip saddr {source} ip daddr . tcp dport @allow meta mark set 0x4c310001 return
  counter drop
 }}
 chain inbound {{
  meta mark set 0
  ip daddr {source} ip saddr . tcp sport @allow ct state established meta mark set 0x4c310001 return
  counter drop
 }}
 chain post {{ type filter hook postrouting priority 50; policy accept;
  {match} meta mark != 0x4c310002 counter drop
  {reverse} meta mark != 0x4c310002 counter drop
  {match} meta mark set 0
  {reverse} meta mark set 0
 }}
}}
table inet {second} {{
 {sets}
 {local}
 chain pre {{ type filter hook prerouting priority -50; policy accept;
  {match} meta mark set 0
  ip daddr {source} meta mark set 0
 }}
 chain forward {{ type filter hook forward priority 60; policy accept;
  {match} jump outbound
  {reverse} jump inbound
 }}
 chain outbound {{
  meta mark 0x4c310001 ip saddr {source} ip daddr . tcp dport @allow meta mark set 0x4c310002 return
  counter drop
 }}
 chain inbound {{
  meta mark 0x4c310001 ip daddr {source} ip saddr . tcp sport @allow ct state established meta mark set 0x4c310002 return
  counter drop
 }}
}}
'''

    def snapshot(self):
        result = []
        for table in self.tables:
            value = json.loads(self.run('nft', '-j', 'list', 'table', 'inet', table))
            result.append(hashlib.sha256(json.dumps(stable(value), sort_keys=True).encode()).hexdigest())
        return result

    def install(self):
        # Never replace another session's tables, nor flush a host ruleset.
        existing = json.loads(self.run('nft', '-j', 'list', 'tables'))['nftables']
        if any(row.get('table', {}).get('name') in self.tables for row in existing):
            raise ValueError('old filter domain must be reconciled')
        self.run('nft', '-f', '-', data=self.script().encode())
        self.hashes = self.snapshot()

    def check(self):
        try:
            if not self.hashes or self.snapshot() != self.hashes:
                raise ValueError('host policy drift')
        except Exception:
            self.closed = True
            self.revoke()
            raise

    def grant(self, deadline, target=False):
        with self.lock:
            if self.closed:
                raise ValueError('host admission permanently closed')
            return self._grant(deadline, target)

    def _grant(self, deadline, target):
        self.check()
        now = datetime.datetime.now(datetime.timezone.utc)
        end = datetime.datetime.fromisoformat(deadline.replace('Z', '+00:00'))
        ms = int((end - now).total_seconds() * 1000) - 2000
        if not 0 < ms <= 33000:
            raise ValueError('bounded host grant required')
        script = ''
        for table in self.tables:
            # A grant for the other bounded run may be conservatively shortened;
            # this never extends the per-run inner Permit or returns a slot.
            script += f'flush set inet {table} allow\n'
            for ip, port in [(self.resolver, 53)] + ([(self.target, 443)] if target else []):
                script += f'add element inet {table} allow {{ {ip} . {port} timeout {ms}ms }}\n'
        self.run('nft', '-f', '-', data=script.encode())

    def revoke(self):
        with self.lock:
            self._revoke()

    def close(self):
        # Serialize against in-flight grants: shutdown/revocation cannot race
        # a successful Permit callback into reopening the host exception.
        with self.lock:
            self.closed = True
            self._revoke()

    def _revoke(self):
        # Each remaining table independently denies when the other is missing.
        # Do not delete the anchors while a physical domain may still exist.
        for table in self.tables:
            try:
                self.run('nft', '-f', '-', data=f'flush set inet {table} allow\n'.encode())
            except Exception:
                pass

    def retire(self):
        # Only the trusted runtime may call this AFTER original signed physical
        # drain and namespace removal. Missing/drifted anchors stay quarantined.
        with self.lock:
            if not self.closed:
                raise ValueError('live host anchors cannot be retired')
            self.check()
            self.run('nft', '-f', '-', data=''.join(
                f'delete table inet {table}\n' for table in self.tables).encode())
            self.hashes = None
