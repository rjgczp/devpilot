The legacy stdout is base64-encoded to preserve its exact bytes and whitespace.
These are reproduced command outputs, not the lost stdout from the retired
Lighthouse namespace. The binary was copied read-only from that host:

- Ubuntu package: iproute2 5.15.0-1ubuntu2.
- `/usr/sbin/tc` SHA256: e502fa3246a97fb6e54329d5ea29ff3278735b0d1dcfb05bf820dcf3ab45e012.
- A network-none Linux test container created only one owned network namespace
  and a fresh veth pair, then used the unchanged TCGuard installation commands.
- `tc -j filter show dev lite0 egress` returned 0 with empty stderr. Its stdout
  failed strict JSON at column 433, exactly matching the production journal.
- The legacy fw options contain bare `handle 0x4c310002`; the modern fixture is
  a native-tool read of the same rules. Mark and full mask must be preserved.
- This runs the host's amd64 tool through the ELF loader in the ARM64 lab;
  no original host rules, production tools, budgets or database were changed.

Upstream cause: iproute2 v5.15.0 `tc/f_fw.c` lines 120-129 unconditionally use
fprintf for the handle. The v6.8.0 printer uses a proper `fw` JSON object:
https://github.com/iproute2/iproute2/blob/v5.15.0/tc/f_fw.c
https://github.com/iproute2/iproute2/blob/v6.8.0/tc/f_fw.c
