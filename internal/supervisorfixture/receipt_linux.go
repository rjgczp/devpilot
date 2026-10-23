//go:build schedulerfixture && linux

package supervisorfixture

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

// StopReceipt contains no target or credentials. Its digests bind the separate
// raw kernel/process proofs. Verification does not release any database slot.
type StopReceipt struct {
	Payload struct {
		Protocol        string          `json:"protocol"`
		ReceiptID       string          `json:"receipt_id"`
		Lineage         handoff.Lineage `json:"lineage"`
		SupervisorKeyID string          `json:"supervisor_key_id"`
		NetworkSHA256   string          `json:"network_sha256"`
		ProcessSHA256   string          `json:"process_sha256"`
		RetireSHA256    string          `json:"retire_sha256"`
		PIDNamespace    string          `json:"pid_namespace"`
		NetNamespace    string          `json:"net_namespace"`
		ProcessCount    int             `json:"process_count"`
		At              time.Time       `json:"at"`
	} `json:"payload"`
	Signature string `json:"signature"`
}

// VerifyStopReceipt requires an independently pinned management public key;
// a public key carried by an untrusted Worker/receipt cannot establish trust.
// Replay consumption/persistence is outside this pure verifier (task5.4).
func VerifyStopReceipt(raw []byte, publicPEM []byte, expected handoff.Lineage) error {
	denied := errors.New("fixture_termination_unverified")
	var receipt StopReceipt
	if len(raw) > 4096 || Decode(raw, &receipt) != nil || !expected.Valid() || receipt.Payload.Lineage != expected {
		return denied
	}
	p := receipt.Payload
	nonce := expected
	nonce.RunID = p.ReceiptID
	if !nonce.Valid() {
		return denied
	}
	if p.Protocol != "fixture-physical-stop-v1" || len(p.ReceiptID) != 36 || p.ProcessCount < 1 || p.ProcessCount > 127 || p.At.IsZero() || p.PIDNamespace == "" || p.NetNamespace == "" {
		return denied
	}
	keyHash := sha256.Sum256(publicPEM)
	if p.SupervisorKeyID != hex.EncodeToString(keyHash[:]) {
		return denied
	}
	for _, s := range []string{p.NetworkSHA256, p.ProcessSHA256, p.RetireSHA256} {
		b, e := hex.DecodeString(s)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != s {
			return denied
		}
	}
	block, rest := pem.Decode(publicPEM)
	if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return denied
	}
	key, e := x509.ParsePKIXPublicKey(block.Bytes)
	if e != nil {
		return denied
	}
	ed, ok := key.(ed25519.PublicKey)
	if !ok {
		return denied
	}
	var frame struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return denied
	}
	// The signing protocol uses sorted-key, compact UTF-8 JSON. Re-encode a map,
	// retaining original string values (not time.Time formatting or field order).
	var payload map[string]any
	if json.Unmarshal(frame.Payload, &payload) != nil {
		return denied
	}
	message, e := json.Marshal(payload)
	if e != nil {
		return denied
	}
	signature, e := base64.StdEncoding.Strict().DecodeString(receipt.Signature)
	if e != nil || !ed25519.Verify(ed, message, signature) {
		return denied
	}
	return nil
}
