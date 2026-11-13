package plugin

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/poizenjam/aida64sd/internal/aida64"
	"github.com/poizenjam/aida64sd/pkg/service"
)

// wantCounts is the expected category layout of the live dump fixture
// (internal/aida64/testdata/real_dump.txt), pinned by the parser's
// TestRealDump as well: 54 readings = 13 temp, 18 volt, 7 fan, 2 duty,
// 5 curr, 9 pwr, no sys.
var wantCounts = map[string]int{
	"temp": 13,
	"volt": 18,
	"fan":  7,
	"duty": 2,
	"curr": 5,
	"pwr":  9,
}

var wantOrder = []string{"temp", "volt", "fan", "duty", "curr", "pwr"}

const fixturePath = "../testdata/real_dump.txt"

// newTestService points Reader at the committed fixture.
func newTestService(t *testing.T) *Service {
	t.Helper()
	orig := Reader
	t.Cleanup(func() { Reader = orig })
	Reader = func() ([]byte, error) { return os.ReadFile(fixturePath) }
	s := NewService()
	s.now = func() time.Time { return time.Unix(1_000_000, 0) }
	return s
}

func TestFirstTickCachesSnapshotAndToken(t *testing.T) {
	s := newTestService(t)
	s.Tick()

	want := uint64(time.Unix(1_000_000, 0).UnixNano())
	if pt, err := s.PollTime(); err != nil || pt != want {
		t.Fatalf("PollTime = (%d, %v), want (%d, nil)", pt, err, want)
	}
	if !s.Available() {
		t.Error("Available = false after successful tick, want true")
	}

	cats := s.Categories()
	if len(cats) != len(wantCounts) {
		t.Fatalf("got %d categories %v, want %d", len(cats), catIDs(cats), len(wantCounts))
	}
	// Display order must be the fixed CategoryOrder, not the block's own
	// order (the dump runs temp/fan/duty/volt/curr/pwr).
	if got := catIDs(cats); !equalStrings(got, wantOrder) {
		t.Errorf("category order = %v, want %v", got, wantOrder)
	}
	for _, c := range cats {
		if want, ok := wantCounts[c.ID]; ok && len(c.Readings) != want {
			t.Errorf("category %s: %d readings, want %d", c.ID, len(c.Readings), want)
		}
		if c.Name == "" {
			t.Errorf("category %s: empty display name", c.ID)
		}
	}
}

func TestTokenMovesOnlyOnContentChange(t *testing.T) {
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	orig := Reader
	t.Cleanup(func() { Reader = orig })
	buf := make([]byte, len(fixture))
	copy(buf, fixture)
	Reader = func() ([]byte, error) { return buf, nil }
	s := NewService()
	s.now = func() time.Time { return time.Unix(1_000_000, 0) }
	s.Tick()
	first, _ := s.PollTime()

	// Same content, one second later: the token must NOT move. This is
	// exactly the idle-box case the 60 s plugin staleness window exists for.
	s.now = func() time.Time { return time.Unix(1_000_001, 0) }
	s.Tick()
	if pt, _ := s.PollTime(); pt != first {
		t.Errorf("token moved on identical content: %d -> %d", first, pt)
	}

	// Changed content: the token stamps the new now. Bump one value digit
	// (TMOBO 33 -> 43) so the XML stays valid and the parse succeeds.
	i := bytes.Index(buf, []byte("<value>33</value>"))
	if i < 0 {
		t.Fatal("fixture layout changed: <value>33</value> not found")
	}
	buf[i+len("<value>")] = '4'
	s.now = func() time.Time { return time.Unix(1_000_002, 0) }
	s.Tick()
	if pt, _ := s.PollTime(); pt != uint64(time.Unix(1_000_002, 0).UnixNano()) {
		t.Errorf("token = %d, want %d (content changed)", pt, uint64(time.Unix(1_000_002, 0).UnixNano()))
	}
}

var errFileNotFound = errors.New("file not found")

func TestReadFailureKeepsSnapshot(t *testing.T) {
	s := newTestService(t)
	s.Tick()
	first, _ := s.PollTime()

	Reader = func() ([]byte, error) { return nil, errFileNotFound }
	s.Tick()
	if s.Available() {
		t.Error("Available = true after read failure, want false")
	}
	if pt, _ := s.PollTime(); pt != first {
		t.Errorf("PollTime changed on read failure: %d -> %d", first, pt)
	}
	if len(s.Categories()) == 0 {
		t.Error("snapshot lost after read failure")
	}

	// Recovery: the next good read re-opens the connection. The content is
	// unchanged, so the token still must not move.
	Reader = func() ([]byte, error) { return os.ReadFile(fixturePath) }
	s.Tick()
	if !s.Available() {
		t.Error("Available = false after recovery tick, want true")
	}
	if pt, _ := s.PollTime(); pt != first {
		t.Errorf("token moved on identical content after recovery: %d -> %d", first, pt)
	}
}

func TestReadingsForCategory(t *testing.T) {
	s := newTestService(t)
	s.Tick()

	readings, err := s.ReadingsForCategory("pwr")
	if err != nil {
		t.Fatalf("ReadingsForCategory(pwr): %v", err)
	}
	var tdp *aida64.Reading
	for i := range readings {
		if readings[i].ID == "PGPU1TDPP" {
			tdp = &readings[i]
		}
	}
	if tdp == nil {
		t.Fatal("PGPU1TDPP missing from pwr readings")
	}
	// The %-labelled power reading must be classified Usage even in pwr.
	if tdp.Type != aida64.TypeUsage || tdp.Type.UnitFor() != "%" {
		t.Errorf("PGPU1TDPP: type %s unit %q, want Usage / %%", tdp.Type, tdp.Type.UnitFor())
	}

	if _, err := s.ReadingsForCategory("sys"); err == nil {
		t.Error("expected error for absent category sys, got nil")
	}
	if _, err := s.ReadingsForCategory("bogus"); err == nil {
		t.Error("expected error for unknown category bogus, got nil")
	}
}

func TestPluginRPCSurface(t *testing.T) {
	s := newTestService(t)
	s.Tick()
	p := &Plugin{Service: s}

	sensors, err := p.Sensors()
	if err != nil {
		t.Fatalf("Sensors: %v", err)
	}
	if len(sensors) != len(wantCounts) {
		t.Fatalf("got %d sensors, want %d", len(sensors), len(wantCounts))
	}
	for _, sn := range sensors {
		if sn.SourceID() != service.LocalSourceID {
			t.Errorf("sensor %s: sourceID %q, want %q", sn.ID(), sn.SourceID(), service.LocalSourceID)
		}
	}
	if sensors[0].ID() != "temp" || sensors[0].Name() != "Temperatures" {
		t.Errorf("first sensor = (%s, %s), want (temp, Temperatures)", sensors[0].ID(), sensors[0].Name())
	}

	sources, err := p.Sources()
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID() != service.LocalSourceID || !sources[0].Available() || sources[0].Name() != "Local" {
		t.Errorf("unexpected sources: %+v", sources)
	}
	if sources[0].PollTime() == 0 {
		t.Error("source pollTime = 0 after a good tick")
	}

	readings, err := p.ReadingsForSensorID("temp")
	if err != nil {
		t.Fatalf("ReadingsForSensorID(temp): %v", err)
	}
	if len(readings) != wantCounts["temp"] {
		t.Errorf("temp: %d readings, want %d", len(readings), wantCounts["temp"])
	}
	r := readings[0]
	if r.ID() == "" || r.Label() == "" || r.Value() == 0 || r.Unit() != "C" || r.Type() != "Temp" {
		t.Errorf("unexpected reading: id=%q label=%q value=%v unit=%q type=%q", r.ID(), r.Label(), r.Value(), r.Unit(), r.Type())
	}
}

func TestEmptyBeforeFirstTick(t *testing.T) {
	s := NewService()
	s.now = func() time.Time { return time.Unix(1_000_000, 0) }
	if pt, _ := s.PollTime(); pt != 0 {
		t.Errorf("PollTime before any tick = %d, want 0", pt)
	}
	if cats := s.Categories(); cats != nil {
		t.Errorf("Categories before any tick = %v, want nil", cats)
	}
	if _, err := s.ReadingsForCategory("temp"); err == nil {
		t.Error("expected error before first tick, got nil")
	}
}

func catIDs(cats []Category) []string {
	out := make([]string, 0, len(cats))
	for _, c := range cats {
		out = append(out, c.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
