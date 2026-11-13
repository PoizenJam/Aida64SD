// mock-bridge serves the Aida64SD HardwareService gRPC interface (dispense
// key "aida64plugin") with controllable mock AIDA64 data. It stands in for
// the Windows-only AIDA64_SensorValues bridge during plugin development, so
// the plugin exercises the exact same go-plugin path as production. It builds
// on every platform (no cgo).
//
// It drives the real bridge service (internal/aida64/plugin) with a fake
// block reader: every second the mock random-walks its readings, rebuilds a
// block string in AIDA64 format, and the service decodes it exactly as it
// would decode live shared memory. The 7 mock categories (temp/volt/fan/
// duty/curr/pwr/sys) also exercise the dynamic enumeration and the
// label-% and sys-id classification rules.
//
// Control API (HTTP, port from MOCK_CONTROL_PORT, default 9999):
//
//	GET  /list                    dump the current block and categories
//	POST /set   {"id":"TCPU","value":77}      set one reading (rebuilds block)
//	POST /reset                   restore all defaults
//	POST /idle  {"on":true}       stop mutating the block (idle-box test)
//	POST /down  {"on":true}       make the block unreadable (AIDA64 closed)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"

	aidplugin "github.com/poizenjam/aida64sd/internal/aida64/plugin"
	"github.com/poizenjam/aida64sd/pkg/service"
)

type mockReading struct {
	category string
	id       string
	label    string
	unit     string // informational only; the block carries no units
	cur      float64
	def      float64
	min, max float64
	step     float64
	text     string // when set (e.g. "N/A"), the value is written verbatim
}

// catOrder matches the parser's known category tags; sys is included
// deliberately so the mock covers every branch the real block may hit.
var catOrder = []string{"temp", "volt", "fan", "duty", "curr", "pwr", "sys"}

func defaultReadings() []mockReading {
	return []mockReading{
		{category: "temp", id: "TCPU", label: "CPU Package", unit: "C", def: 50, min: 40, max: 90, step: 2},
		{category: "temp", id: "TGPU1", label: "GPU1", unit: "C", def: 46, min: 35, max: 85, step: 2},
		{category: "temp", id: "TMobo", label: "Motherboard", unit: "C", def: 33, min: 25, max: 60, step: 1},
		{category: "volt", id: "VCPU", label: "CPU Core", unit: "V", def: 1.234, min: 0.9, max: 1.6, step: 0.02},
		{category: "volt", id: "V12V", label: "+12 V", unit: "V", def: 12.1, min: 11.5, max: 12.8, step: 0.05},
		{category: "fan", id: "FCPU", label: "CPU", unit: "RPM", def: 770, min: 400, max: 2500, step: 30},
		{category: "fan", id: "FCHASYS", label: "Chassis", unit: "RPM", def: 650, min: 400, max: 2000, step: 30},
		{category: "duty", id: "DGPU1", label: "GPU1", unit: "%", def: 53, min: 10, max: 100, step: 2},
		{category: "curr", id: "CGPU1PCIE", label: "GPU1 PCIe", unit: "A", def: 1.23, min: 0, max: 8, step: 0.1},
		{category: "pwr", id: "PCPUPKG", label: "CPU Package", unit: "W", def: 74.5, min: 20, max: 150, step: 3},
		{category: "pwr", id: "PGPU1", label: "GPU1", unit: "W", def: 103, min: 30, max: 300, step: 5},
		{category: "pwr", id: "PGPU1TDPP", label: "GPU1 TDP%", unit: "%", def: 26, min: 0, max: 100, step: 1},
		{category: "sys", id: "SCPUCLK", label: "CPU Clock", unit: "MHz", def: 3900, min: 1000, max: 5800, step: 100},
		{category: "sys", id: "SCPUUTI", label: "CPU Usage", unit: "%", def: 37, min: 0, max: 100, step: 2},
		{category: "sys", id: "SDISK", label: "Disk Queue", unit: "", def: 0, min: 0, max: 0, step: 0, text: "N/A"},
	}
}

type mockStore struct {
	mu       sync.Mutex
	readings []mockReading
	block    []byte
	idle     bool
	down     bool
	rand     *rand.Rand
}

func newMockStore() *mockStore {
	m := &mockStore{rand: rand.New(rand.NewSource(1))}
	m.mu.Lock()
	m.readings = defaultReadings()
	m.block = m.build()
	m.mu.Unlock()
	return m
}

// build renders the current readings as an AIDA64-format block string.
func (m *mockStore) build() []byte {
	var b []byte
	byCat := map[string][]mockReading{}
	for _, cat := range catOrder {
		byCat[cat] = nil
	}
	for _, r := range m.readings {
		byCat[r.category] = append(byCat[r.category], r)
	}
	for _, cat := range catOrder {
		for _, r := range byCat[cat] {
			v := r.text
			if v == "" {
				v = strconv.FormatFloat(r.cur, 'f', -1, 64)
			}
			b = append(b, fmt.Sprintf("<%s><id>%s</id><label>%s</label><value>%s</value></%s>",
				cat, r.id, r.label, v, cat)...)
		}
	}
	return b
}

// walk performs one random-walk step per reading and rebuilds the block.
// Called from the 1 s ticker, mirroring how live AIDA64 rewrites its block.
func (m *mockStore) walk() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idle || m.down {
		return
	}
	for i := range m.readings {
		r := &m.readings[i]
		if r.text != "" {
			continue
		}
		d := (m.rand.Float64()*2 - 1) * r.step
		r.cur += d
		if r.cur < r.min {
			r.cur = r.min
		}
		if r.cur > r.max {
			r.cur = r.max
		}
	}
	m.block = m.build()
}

// read returns a fresh copy of the current block, like the real cgo reader.
func (m *mockStore) read() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down {
		return nil, errors.New("mock-bridge: AIDA64 shared memory not present")
	}
	out := make([]byte, len(m.block))
	copy(out, m.block)
	return out, nil
}

func (m *mockStore) set(id string, v float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.readings {
		if m.readings[i].id == id {
			m.readings[i].cur = v
			m.block = m.build()
			return nil
		}
	}
	return fmt.Errorf("unknown reading id %q", id)
}

func (m *mockStore) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readings = defaultReadings()
	m.idle = false
	m.down = false
	m.block = m.build()
}

// --- control API ---------------------------------------------------------

func main() {
	store := newMockStore()

	// Wire the mock block into the real bridge service before it starts,
	// so the served data goes through the exact decode/cache/token path.
	aidplugin.Reader = store.read
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			store.walk()
		}
	}()

	port := os.Getenv("MOCK_CONTROL_PORT")
	if port == "" {
		port = "9999"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, _ *http.Request) {
		store.mu.Lock()
		block, idle, down := string(store.block), store.idle, store.down
		store.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"block": block, "idle": idle, "down": down,
		})
	})
	mux.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID    string  `json:"id"`
			Value float64 `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.set(req.ID, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		store.reset()
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/idle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		store.mu.Lock()
		store.idle = req.On
		store.mu.Unlock()
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		store.mu.Lock()
		store.down = req.On
		store.mu.Unlock()
		fmt.Fprint(w, `{"ok":true}`)
	})
	go func() {
		// The control server is best-effort: a bind failure (port in use
		// after a restart) must not take down the gRPC side.
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			log.Printf("mock-bridge control server: %v", err)
		}
	}()

	svc := aidplugin.NewService()
	svc.Start()

	log.Printf("mock-bridge: control API on :%s (AIDA64 not required)", port)
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: service.Handshake,
		Plugins: map[string]plugin.Plugin{
			service.PluginKey: &service.Aida64ServicePlugin{
				Impl: &aidplugin.Plugin{Service: svc},
			},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
