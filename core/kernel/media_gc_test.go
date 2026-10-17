package kernel

import (
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/features/collab"
	"github.com/hydrogencms/hydrogen/features/media"
)

// Garbage collection does not read unsaved collaborative state or relayed drafts, so the windows in which those
// can hold the only reference to an upload must end long before the collection may remove it.
func TestMediaGCAgeOutlivesCollabAndRelayWindows(t *testing.T) {
	if media.DefaultGCAge < media.MinGCAge {
		t.Fatalf("default GC age %s is below the minimum %s", media.DefaultGCAge, media.MinGCAge)
	}
	for name, window := range map[string]time.Duration{
		"relay ttl": delivery.RelayTTL, "collab session idle": collab.SessionIdle, "collab compact window": collab.CompactWindow,
	} {
		if window*2 > media.MinGCAge {
			t.Errorf("%s (%v) is not much shorter than the minimum GC age %s", name, window, media.MinGCAge)
		}
	}
}
