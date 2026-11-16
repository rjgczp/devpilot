//go:build privateworker && privateprobe && !schedulerfixture && linux

package supervisorfixture

// The private service shares the accepted physical verifier, not BootFixture.
// Its process is cap0; namespace administration stays in the trusted parent.
import (
	"encoding/json"
	"github.com/rjgczp/devpilot/internal/probenetwork"
)

type NetworkStop = probenetwork.NetworkStop

var ErrDenied = probenetwork.ErrDenied
var Decode = probenetwork.Decode

func uniqueJSON(d *json.Decoder, depth int) error { return probenetwork.UniqueJSON(d, depth) }
