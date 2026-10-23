//go:build schedulerfixture && linux

package supervisorfixture

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"
)

func TestStopBundleRequiresPhysicalProofSemantics(t *testing.T) {
	// Parser/crypto unit only. These synthetic objects are not physical evidence.
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(pub)
	key := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	kh := sha256.Sum256(key)
	l := testLineage()
	for _, mutate := range []string{"valid", "missing-pidfd", "duplicate-pid", "live-process", "unconsumed-input", "wrong-retirement", "wrong-network", "network-after-process", "unbounded-stop", "missing-bundle"} {
		t.Run(mutate, func(t *testing.T) {
			at := time.Now().UTC()
			network := []NetworkStop{{Lineage: l, Namespace: "net:[42]", Role: "run", RulesHash: testConfig().RouteHash, LinkRemoved: true, ConntrackEmpty: true, StoppedAt: at}, {Lineage: l, Namespace: "net:[43]", Role: "gateway", RulesHash: testConfig().RouteHash, LinkRemoved: true, ConntrackEmpty: true, StoppedAt: at}}
			process := processStop{Event: "processes_terminated", Lineage: l, NetNamespace: "net:[42]", PIDNamespace: "pid:[44]", Remaining: []int{}, LaunchConsumed: true, At: at.UnixNano() + 1000000, Started: 1, Finished: 2}
			_ = json.Unmarshal([]byte(`[{"pid":2,"start_ticks":"10","pidfd_exited":true},{"pid":3,"start_ticks":"11","pidfd_exited":true}]`), &process.PIDs)
			retired := retiredStop{Event: "pid_namespace_retiring", PIDNamespace: "pid:[44]", Lineage: l}
			switch mutate {
			case "missing-pidfd":
				process.PIDs[0].Exited = false
			case "duplicate-pid":
				process.PIDs[1].PID = 2
			case "live-process":
				process.Remaining = []int{7}
			case "unconsumed-input":
				process.LaunchConsumed = false
			case "wrong-retirement":
				retired.PIDNamespace = "pid:[99]"
			case "wrong-network":
				network[1].Role = "run"
			case "network-after-process":
				network[0].StoppedAt = at.Add(time.Second)
			case "unbounded-stop":
				process.Finished = int64(3 * time.Second)
			}
			n, _ := json.Marshal(network)
			p, _ := json.Marshal(process)
			r, _ := json.Marshal(retired)
			payload := map[string]any{"protocol": "fixture-physical-stop-v1", "receipt_id": "01900000-0000-7000-8000-000000000009", "lineage": l, "supervisor_key_id": hex.EncodeToString(kh[:]), "network_sha256": proofDigest(n), "process_sha256": proofDigest(p), "retire_sha256": proofDigest(r), "pid_namespace": "pid:[44]", "net_namespace": "net:[42]", "process_count": 2, "at": at.Format(time.RFC3339Nano)}
			raw, _ := json.Marshal(payload)
			var canonicalPayload any
			_ = json.Unmarshal(raw, &canonicalPayload)
			message, _ := json.Marshal(canonicalPayload)
			receipt, _ := json.Marshal(map[string]any{"payload": payload, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, message))})
			bundle, _ := json.Marshal(map[string]json.RawMessage{"network": n, "process": p, "retired": r})
			if mutate == "missing-bundle" {
				bundle = nil
			}
			if VerifyStopReceipt(receipt, key, l) != nil {
				t.Fatal("unit did not provide a valid signature")
			}
			v, e := VerifyCommittedStopBundle(receipt, key, bundle, l)
			if (e == nil) != (mutate == "valid") {
				t.Fatal("physical semantics gate", mutate, e)
			}
			if e == nil && (v.ReceiptID() == "" || len(v.Digest()) != 32) {
				t.Fatal("missing fixed reconciliation identity")
			}
		})
	}
}
func TestStopBundleDigestRetainsIntegerPrecision(t *testing.T) {
	raw := []byte(`{"b":1700000000000000001,"a":1}`)
	want := sha256.Sum256([]byte(`{"a":1,"b":1700000000000000001}`))
	if proofDigest(raw) != hex.EncodeToString(want[:]) {
		t.Fatal("nanosecond proof integers rounded during canonicalization")
	}
}
