//go:build privateworker && privateprobe && linux

// Operator-only DNS review/offline approval. This is not an API or Probe entry.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"io"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"time"
)

func run() error {
	if len(os.Args) < 2 {
		return errors.New("action required")
	}
	f := flag.NewFlagSet("privateapproval", flag.ContinueOnError)
	path := f.String("request-file", "", "operator request JSON file")
	if f.Parse(os.Args[2:]) != nil || f.NArg() != 0 {
		return errors.New("flags")
	}
	in, e := os.Open(*path)
	if e != nil {
		return errors.New("request")
	}
	defer in.Close()
	b, e := io.ReadAll(io.LimitReader(in, 16385))
	if e != nil || len(b) > 16384 || !credentials.ValidJSON(b) {
		return errors.New("request")
	}
	var request struct {
		Approval scheduler.TargetApproval `json:"approval"`
		Denied   []netip.Prefix           `json:"denied_prefixes"`
		Review   monitoring.DNSReview     `json:"review"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil {
		return errors.New("request")
	}
	switch os.Args[1] {
	case "review":
		r, e := monitoring.ReviewPrivateDNSDeny(context.Background(), request.Approval.URL, request.Denied)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	case "approve":
		a := request.Approval
		r := request.Review
		u, e := url.Parse(a.URL)
		if e != nil || u.Hostname() != r.Host || !slices.Contains(r.Records, "HOST|"+r.Host) {
			return errors.New("host binding")
		}
		if r.ObservedAt.After(time.Now()) || time.Since(r.ObservedAt) > 30*time.Second {
			return errors.New("stale review")
		}
		a.DNSDigest = monitoring.DNSBinding(r.Records)
		a.Addresses = r.Addresses
		a.ApprovedAt = time.Now().UTC()
		// Expiry must be an explicit operator choice; bounded to24h by the same
		// directory validation used by Worker. No network or key rotation here.
		if !a.ExpiresAt.After(a.ApprovedAt) {
			return errors.New("expiry")
		}
		_, e = scheduler.NewApprovedDirectory(map[int64][]byte{1: bytes.Repeat([]byte{1}, 32)}, []scheduler.TargetApproval{a})
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(a)
	default:
		return errors.New("action")
	}
}
func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "operator approval rejected")
		os.Exit(2)
	}
}
