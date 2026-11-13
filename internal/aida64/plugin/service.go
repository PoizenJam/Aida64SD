package plugin

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/poizenjam/aida64sd/internal/aida64"
	"github.com/poizenjam/aida64sd/pkg/service"
)

// PollInterval is the bridge's fixed pull interval. It is far shorter than
// AIDA64's typical 1 s sensor-update rate so the content-change token
// (lastChange) advances one step per real AIDA64 write: a read period equal to
// the write period races it and detects a change only every other read,
// aliasing a 1 s cadence into 2 s. The cost is a MapViewOfFile copy of a
// few-KB block 10x/s; the host pulls the cached value over gRPC on its own
// (separate, user-adjustable) ticker, so nothing extra is pushed.
const PollInterval = 100 * time.Millisecond

// Category is a sensor group in the bridge's snapshot: one AIDA64 category
// tag (temp/volt/...) and the readings decoded from it. This is the
// "sensor" layer of the sensor/reading model; the tags are the "readings".
type Category struct {
	ID       string
	Name     string
	Readings []aida64.Reading
}

// snapshot is one decoded pull of the shared-memory block.
type snapshot struct {
	categories []Category
	byID       map[string][]aida64.Reading
}

// Service pulls the AIDA64_SensorValues block on a fixed 1 s ticker and
// caches the latest decoded snapshot plus a freshness token. The token
// mirrors the HWiNFO dwLastUpdate contract: AIDA64 has no embedded poll
// time, so the bridge stamps the time the block content last CHANGED
// (bytes.Compare against the raw bytes). An idle box can have AIDA64 rewrite
// byte-identical content for many seconds, which is why the plugin applies a
// 60 s staleness window to the token rather than HWiNFO's 5 s.
type Service struct {
	mu sync.RWMutex
	// prevRaw is the last successfully read block, kept to detect content
	// changes. Compared raw (not decoded) because two different blocks can
	// decode to the same readings.
	prevRaw []byte
	// snap is the last successfully parsed snapshot; it survives read
	// failures so PollTime keeps reporting the last (stale) token and the
	// plugin decides on staleness.
	snap       *snapshot
	lastChange uint64 // Unix ns; 0 until the first successful read
	available  bool   // block opened and parsed on the last tick
	now        func() time.Time
}

// NewService creates the bridge service. Reader (the cgo reader on
// Windows) is wired in by the build-tagged reader_*.go files.
func NewService() *Service {
	return &Service{now: time.Now}
}

// Start launches the 1 s ticker as a single goroutine that runs for the
// bridge's whole lifetime (no cancel; the process exit ends it). The initial
// pull is immediate so the cache is warm at startup.
func (s *Service) Start() {
	go func() {
		s.Tick()
		t := time.NewTicker(PollInterval)
		defer t.Stop()
		for range t.C {
			s.Tick()
		}
	}()
}

// Tick performs one pull: read the block, decode it, update the cache and
// the freshness token. Exported for tests; the ticker is the only
// production caller.
func (s *Service) Tick() {
	buf, err := Reader()
	if err != nil {
		// The block vanishes while AIDA64 is closed or restarting. Keep the
		// previous snapshot; mark the connection down.
		s.mu.Lock()
		s.available = false
		s.mu.Unlock()
		return
	}
	readings, err := aida64.DecodeBytes(buf)
	if err != nil {
		s.mu.Lock()
		s.available = false
		s.mu.Unlock()
		return
	}

	now := uint64(s.now().UnixNano())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.available = true

	changed := s.snap == nil || !bytes.Equal(s.prevRaw, buf)
	if changed {
		s.lastChange = now
	}
	// Copy: the reader may hand back a reusable buffer (the cgo reader
	// returns a fresh one, but the mock and future readers need not), and
	// prevRaw must survive the reader's next write to stay comparable.
	s.prevRaw = append([]byte(nil), buf...)

	byCat := map[string][]aida64.Reading{}
	order := make([]string, 0, len(byCat))
	for _, r := range readings {
		c := string(r.Category)
		if _, ok := byCat[c]; !ok {
			order = append(order, c)
		}
		byCat[c] = append(byCat[c], r)
	}
	service.SortCategories(order)

	snap := &snapshot{byID: make(map[string][]aida64.Reading, len(order))}
	for _, c := range order {
		snap.categories = append(snap.categories, Category{
			ID:       c,
			Name:     service.CategoryName(c),
			Readings: byCat[c],
		})
		snap.byID[c] = byCat[c]
	}
	s.snap = snap
}

// PollTime returns the freshness token: Unix ns at which the block content
// last changed, 0 until then. Same contract shape as HWiNFO's PollTime.
func (s *Service) PollTime() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastChange, nil
}

// Categories lists the sensor groups of the latest snapshot, ordered for
// display (nil until the first successful parse).
func (s *Service) Categories() []Category {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snap == nil {
		return nil
	}
	return s.snap.categories
}

// ReadingsForCategory returns the readings of one category group.
func (s *Service) ReadingsForCategory(id string) ([]aida64.Reading, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snap == nil {
		return nil, fmt.Errorf("no snapshot: AIDA64 shared memory not read yet")
	}
	readings, ok := s.snap.byID[id]
	if !ok {
		return nil, fmt.Errorf("category %q not present in latest snapshot", id)
	}
	return readings, nil
}

// Available reports whether the block was opened and parsed on the last tick
// (connection status, as opposed to content freshness).
func (s *Service) Available() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.available
}
