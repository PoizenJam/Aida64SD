package aida64plugin

import (
	"testing"

	"github.com/poizenjam/aida64sd/pkg/streamdeck"
)

// A closed PI must stop receiving status pushes; the tile itself is untouched.
func TestPropertyInspectorDisappearedPrunesContext(t *testing.T) {
	p := &Plugin{piContexts: map[string]string{"ctxA": "com.poizenjam.aida64sd.reading", "ctxB": "com.poizenjam.aida64sd.reading"}}
	ev := &streamdeck.EvSendToPlugin{Action: "com.poizenjam.aida64sd.reading", Context: "ctxA"}
	p.OnPropertyInspectorDisappeared(ev)
	p.OnPropertyInspectorDisappeared(ev) // DidDisappear + WillDisappear both arrive
	if _, ok := p.piContexts["ctxA"]; ok {
		t.Fatal("ctxA still registered after its PI disappeared")
	}
	if _, ok := p.piContexts["ctxB"]; !ok {
		t.Fatal("ctxB pruned, but only ctxA's PI disappeared")
	}
}
