#!/usr/bin/env python3
"""Separate execution policy; never modifies the configuration API classifier."""
import ipaddress
import json
import re
import sys

ALLOWED = {('127.0.0.53',53),('93.184.216.34',80),('93.184.216.34',443),
           ('93.184.216.35',80),('93.184.216.37',80),('93.184.216.38',443),
           ('2606:4700:4700::1111',80)}

def classify(lines):
    attempts, violations = [], []
    for line in lines:
        if not re.search(r'\b(connect|sendto|sendmsg|sendmmsg)\(',line):
            continue
        if 'resumed>' in line:
            continue
        if 'AF_UNIX' in line or 'AF_LOCAL' in line:
            violations.append('Unix socket attempt');continue
        if 'AF_INET' not in line:
            if 'AF_UNSPEC' in line or 'msg_name=NULL' in line or re.search(r', NULL, 0\)',line):
                continue
            violations.append('unknown destination');continue
        v4 = re.findall(r'sin_port=htons\((\d+)\), sin_addr=inet_addr\("([^"]+)"\)',line)
        v6 = re.findall(r'sin6_port=htons\((\d+)\).*?inet_pton\(AF_INET6, "([^"]+)"',line)
        tuples = v6 if 'AF_INET6' in line else v4
        if not tuples:
            violations.append('unparsed IP destination');continue
        for port,ip in tuples:
            try:
                item=(str(ipaddress.ip_address(ip)),int(port))
            except ValueError:
                violations.append('invalid IP');continue
            attempts.append(item)
            if item not in ALLOWED or not re.search(r'<TCP(?:v6)?:\[',line):
                violations.append(f'unapproved attempt {item[0]}:{item[1]}')
    return dict(attempts=attempts,violations=violations,
                dns_attempts=sum((ip,p)==('127.0.0.53',53) for ip,p in attempts),
                target_attempts=sum((ip,p)!=('127.0.0.53',53) for ip,p in attempts))

if __name__=='__main__':
    with open(sys.argv[1]) as trace:
        report=classify(trace)
    print(json.dumps(report,indent=2))
    sys.exit(1 if report['violations'] else 0)
