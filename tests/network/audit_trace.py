#!/usr/bin/env python3
"""Classify attempted destinations, including failed connect/send syscalls.

Only the exact disposable TCP DB and HTTP fixture tuples are approved. This
parser is fail closed for unknown destination formats and Unix sockets; it does
not treat failure, timeout or lack of response as evidence of no attempt.
"""
import re
import sys

APPROVED = {("127.0.0.1", "5432"), ("127.0.0.1", "18081")}
violations = []
approved = 0
for line in open(sys.argv[1], encoding="utf-8"):
    if not re.search(r"\b(connect|sendto|sendmsg|sendmmsg)\(", line):
        continue
    # A NULL destination uses an already connected socket, whose connect was
    # audited. AF_UNSPEC disconnects cannot select a new destination.
    if "AF_UNIX" in line or "AF_LOCAL" in line:
        violations.append("unapproved Unix socket destination")
        continue
    if "AF_INET" not in line:
        if "AF_UNSPEC" in line or "msg_name=NULL" in line or re.search(r", NULL, 0\)", line):
            continue
        if "resumed>" in line:
            continue
        violations.append("unclassified destination syscall")
        continue
    tuples = re.findall(r'sin_port=htons\((\d+)\), sin_addr=inet_addr\("([^"]+)"\)', line)
    if "AF_INET6" in line or not tuples:
        violations.append("unapproved IPv6 or unclassified IP destination")
        continue
    for port, address in tuples:
        if (address, port) in APPROVED and "<TCP:[" in line:
            approved += 1
        else:
            violations.append(f"unapproved attempted destination {address}:{port}")
print(f"approved_attempts={approved} blocked_attempts={len(violations)}")
for violation in violations:
    print(violation)
sys.exit(1 if violations else 0)
