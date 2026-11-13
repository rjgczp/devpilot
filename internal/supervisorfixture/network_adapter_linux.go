//go:build schedulerfixture && linux

package supervisorfixture

import (
	"encoding/json"
	"github.com/rjgczp/devpilot/internal/probenetwork"
)

// Reviewed fixture adapter. Private services do not import this package.
type Config = probenetwork.Config
type Install = probenetwork.Install
type Ready = probenetwork.Ready
type Policy = probenetwork.Policy
type NetworkStop = probenetwork.NetworkStop

var ErrDenied = probenetwork.ErrDenied
var FailureCode = probenetwork.FailureCode
var Fingerprints = probenetwork.Fingerprints
var Boot = probenetwork.BootFixture
var Decode = probenetwork.Decode
var Revoke = probenetwork.Revoke

func uniqueJSON(d *json.Decoder, depth int) error { return probenetwork.UniqueJSON(d, depth) }
