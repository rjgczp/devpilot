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
	"strings"
	"testing"
	"time"
)

func TestFixtureStopReceiptPinsKeyAndEntireLineage(t *testing.T) {
	// Cryptographic parser unit only; synthetic digests are never Linux evidence.
	public, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.MarshalPKIXPublicKey(public)
	if e != nil {
		t.Fatal(e)
	}
	key := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	keyHash := sha256.Sum256(key)
	lineage := testLineage()
	payload := map[string]any{"protocol": "fixture-physical-stop-v1", "receipt_id": "01900000-0000-7000-8000-000000000007", "lineage": lineage, "supervisor_key_id": hex.EncodeToString(keyHash[:]), "network_sha256": testConfig().RouteHash, "process_sha256": testConfig().RouteHash, "retire_sha256": testConfig().AddressHash, "pid_namespace": "pid:[42]", "net_namespace": "net:[43]", "process_count": 3, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	message, _ := json.Marshal(payload)
	var canonicalPayload map[string]any
	_ = json.Unmarshal(message, &canonicalPayload)
	message, _ = json.Marshal(canonicalPayload)
	frame := map[string]any{"payload": payload, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(private, message))}
	raw, _ := json.Marshal(frame)
	if VerifyStopReceipt(raw, key, lineage) != nil {
		t.Fatal("valid pinned signature rejected")
	}
	for _, p := range []*string{&lineage.WorkspaceID, &lineage.ProjectID, &lineage.MonitorID, &lineage.RunID, &lineage.NamespaceIncarnation, &lineage.SupervisorIncarnation, &lineage.ConfigurationVersion, &lineage.ActivationGeneration, &lineage.SchedulerEpoch, &lineage.FencingToken} {
		previous := *p
		if strings.Contains(previous, "-") {
			*p = "01900000-0000-7000-8000-000000000008"
		} else {
			*p = "8"
		}
		if VerifyStopReceipt(raw, key, lineage) == nil {
			t.Fatal("mixed lineage accepted")
		}
		*p = previous
	}
	for _, field := range []string{"receipt_id", "network_sha256", "process_sha256", "retire_sha256", "pid_namespace", "net_namespace"} {
		old := payload[field]
		payload[field] = "tampered"
		changed, _ := json.Marshal(frame)
		if VerifyStopReceipt(changed, key, lineage) == nil {
			t.Fatal("tampered proof accepted", field)
		}
		payload[field] = old
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	otherDER, _ := x509.MarshalPKIXPublicKey(other)
	if VerifyStopReceipt(raw, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: otherDER}), lineage) == nil {
		t.Fatal("foreign management key accepted")
	}
}
