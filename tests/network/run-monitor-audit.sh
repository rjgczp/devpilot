#!/bin/sh
# Run only in a disposable Linux network namespace, with NET_ADMIN and ptrace.
# Compile/copy integration.test + pinned Goose BEFORE invoking this script.
# DB must be the isolated test DB at 127.0.0.1:5432; test HTTP is 127.0.0.1:18081.
# The firewall rejects unapproved traffic before it leaves the namespace;
# strace independently records syscall *attempts*, even when rejection is instant.
set -eu
[ "${MONITOR_PROBE_ISOLATED:-}" = 1 ] || { echo 'isolated namespace acknowledgement required'; exit 2; }
[ "$(uname -s)" = Linux ] || exit 2
: "${MONITOR_TEST_BINARY:?compiled integration test binary required}"
: "${MONITOR_AUDIT_OUTPUT:?dedicated evidence directory required}"
mkdir -p "$MONITOR_AUDIT_OUTPUT"
python3 - <<'PY'
import os, urllib.parse
u=urllib.parse.urlsplit(os.environ.get('TEST_DATABASE_URL',''))
assert u.hostname=='127.0.0.1' and (u.port or 5432)==5432
assert u.path=='/devpilot_registration_test'
assert not os.environ.get('DATABASE_URL'), 'no application database allowed'
PY
chain=DEVPILOT_MONITOR_AUDIT
cleanup() {
    iptables -D OUTPUT -j "$chain"
    iptables -F "$chain"
    iptables -X "$chain"
    ip6tables -D OUTPUT -j "$chain"
    ip6tables -F "$chain"
    ip6tables -X "$chain"
}
iptables -N "$chain"
ip6tables -N "$chain"
trap cleanup EXIT INT TERM
iptables -A "$chain" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A "$chain" -p tcp -d 127.0.0.1 --dport 5432 -j ACCEPT
iptables -A "$chain" -p tcp -d 127.0.0.1 --dport 18081 -j ACCEPT
iptables -A "$chain" -j REJECT
ip6tables -A "$chain" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
ip6tables -A "$chain" -j REJECT
iptables -I OUTPUT 1 -j "$chain"
ip6tables -I OUTPUT 1 -j "$chain"
export MONITOR_PROBE_HTTP_ADDR=127.0.0.1:18081
for mode in control real; do
    testname=TestMonitorNoTargetNetwork
    [ "$mode" = real ] || testname=TestMonitorNetworkProbePositiveControl
    # Keep command and child exit status alongside syscall and classifier output.
    echo "$MONITOR_TEST_BINARY -test.run ^$testname\$ -test.v -test.timeout=60s" > "$MONITOR_AUDIT_OUTPUT/$mode.command"
    set +e
    strace -f -qq -yy -s 256 -e trace=connect,sendto,sendmsg,sendmmsg -o "$MONITOR_AUDIT_OUTPUT/$mode.trace" \
        "$MONITOR_TEST_BINARY" -test.run "^$testname\$" -test.v -test.timeout=60s > "$MONITOR_AUDIT_OUTPUT/$mode.test.txt" 2>&1
    child=$?
    python3 tests/network/audit_trace.py "$MONITOR_AUDIT_OUTPUT/$mode.trace" > "$MONITOR_AUDIT_OUTPUT/$mode.audit.txt"
    audit=$?
    set -e
    echo "child_exit=$child audit_exit=$audit" | tee "$MONITOR_AUDIT_OUTPUT/$mode.exit.txt"
    cat "$MONITOR_AUDIT_OUTPUT/$mode.audit.txt"
    [ "$child" = 0 ] || { cat "$MONITOR_AUDIT_OUTPUT/$mode.test.txt"; exit 2; }
    if [ "$mode" = control ]; then
        # Require an actual recorded violation, not any generic script failure.
        [ "$audit" = 1 ] && grep -q '192.0.2.1:80' "$MONITOR_AUDIT_OUTPUT/$mode.audit.txt" || exit 2
        echo 'positive control: probe rejected the intentionally violating run'
    else
        [ "$audit" = 0 ] || exit 1
        grep -q 'approved_attempts=[1-9]' "$MONITOR_AUDIT_OUTPUT/$mode.audit.txt" || exit 2
        echo 'real Monitor endpoints: zero unapproved target attempts'
    fi
done
