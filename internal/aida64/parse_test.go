package aida64

import (
	"os"
	"testing"
)

// sample mirrors the documented AIDA64_SensorValues content: a flat run of
// category tags, each carrying id/label/value, no root element, NUL-terminated.
const sample = `<temp><id>TCPU</id><label>CPU Package</label><value>52.5</value></temp>` +
	`<volt><id>VCPU</id><label>CPU Core</label><value>1.164</value></volt>` +
	`<fan><id>FCPU</id><label>CPU Fan</label><value>1450</value></fan>` +
	`<duty><id>FCPUDU</id><label>CPU Fan Duty</label><value>42</value></duty>` +
	`<pwr><id>PCPU</id><label>CPU Package Power</label><value>88.3</value></pwr>` +
	`<sys><id>SCPUCLK</id><label>CPU Clock</label><value>3900</value></sys>` +
	`<sys><id>SCPUUTI</id><label>CPU Usage</label><value>37</value></sys>` +
	`<sys><id>SDISK</id><label>System Disk</label><value>0</value></sys>` +
	"\x00extra trailing garbage"

func TestDecode(t *testing.T) {
	readings, err := DecodeString(sample)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if len(readings) != 8 {
		t.Fatalf("got %d readings, want 8", len(readings))
	}

	byID := map[string]Reading{}
	for _, r := range readings {
		byID[r.ID] = r
	}

	cases := []struct {
		id    string
		cat   Category
		typ   Type
		unit  string
		val   float64
		label string
	}{
		{"TCPU", CategoryTemp, TypeTemp, "C", 52.5, "CPU Package"},
		{"VCPU", CategoryVolt, TypeVolt, "V", 1.164, "CPU Core"},
		{"FCPU", CategoryFan, TypeFan, "RPM", 1450, "CPU Fan"},
		{"FCPUDU", CategoryDuty, TypeUsage, "%", 42, "CPU Fan Duty"},
		{"PCPU", CategoryPwr, TypePower, "W", 88.3, "CPU Package Power"},
		{"SCPUCLK", CategorySys, TypeClock, "MHz", 3900, "CPU Clock"},
		{"SCPUUTI", CategorySys, TypeUsage, "%", 37, "CPU Usage"},
		{"SDISK", CategorySys, TypeOther, "", 0, "System Disk"},
	}
	for _, c := range cases {
		r, ok := byID[c.id]
		if !ok {
			t.Errorf("missing reading %s", c.id)
			continue
		}
		if r.Category != c.cat {
			t.Errorf("%s: category %q, want %q", c.id, r.Category, c.cat)
		}
		if r.Type != c.typ {
			t.Errorf("%s: type %v, want %v", c.id, r.Type, c.typ)
		}
		if r.Type.UnitFor() != c.unit {
			t.Errorf("%s: unit %q, want %q", c.id, r.Type.UnitFor(), c.unit)
		}
		if r.Value != c.val {
			t.Errorf("%s: value %v, want %v", c.id, r.Value, c.val)
		}
		if r.Label != c.label {
			t.Errorf("%s: label %q, want %q", c.id, r.Label, c.label)
		}
	}
}

func TestDecodeEmpty(t *testing.T) {
	for _, in := range []string{"", "\x00"} {
		readings, err := DecodeString(in)
		if err != nil {
			t.Errorf("DecodeString(%q): %v", in, err)
		}
		if readings != nil {
			t.Errorf("DecodeString(%q): got %v, want nil", in, readings)
		}
	}
}

func TestDecodeSkipsUnknownTags(t *testing.T) {
	in := `<weird><id>?</id><label>?</label><value>1</value></weird>` +
		`<temp><id>TMobo</id><label>Motherboard</label><value>40</value></temp>`
	readings, err := DecodeString(in)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if len(readings) != 1 || readings[0].ID != "TMobo" {
		t.Fatalf("got %+v, want only TMobo", readings)
	}
}

func TestDecodeDuplicateIDs(t *testing.T) {
	in := `<fan><id>FSYS</id><label>Fan 1</label><value>1000</value></fan>` +
		`<fan><id>FSYS</id><label>Fan 2</label><value>1200</value></fan>`
	readings, err := DecodeString(in)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("got %d readings, want 2", len(readings))
	}
}

// TestRealDump parses the captured AIDA64 dump (internal/aida64/testdata/real_dump.txt)
// and checks every category is present with correctly typed readings.
func TestRealDump(t *testing.T) {
	buf, err := os.ReadFile("testdata/real_dump.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	readings, err := DecodeBytes(buf)
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}

	byID := map[string]Reading{}
	for _, r := range readings {
		byID[r.ID] = r
	}

	// One fixture per observed category; a regression that drops a tag (as
	// an early parser did for <curr>) fails here.
	cases := []struct {
		id  string
		cat Category
		typ Type
	}{
		{"TMOBO", CategoryTemp, TypeTemp},
		{"FCPU", CategoryFan, TypeFan},
		{"DGPU1", CategoryDuty, TypeUsage},
		{"VCPU", CategoryVolt, TypeVolt},
		{"CGPU1PCIE", CategoryCurr, TypeCurrent},
		{"PCPUPKG", CategoryPwr, TypePower},
		// Percentage label inside the pwr category must classify as Usage.
		{"PGPU1TDPP", CategoryPwr, TypeUsage},
	}
	for _, c := range cases {
		r, ok := byID[c.id]
		if !ok {
			t.Errorf("missing reading %s", c.id)
			continue
		}
		if r.Category != c.cat {
			t.Errorf("%s: category %q, want %q", c.id, r.Category, c.cat)
		}
		if r.Type != c.typ {
			t.Errorf("%s: type %v, want %v", c.id, r.Type, c.typ)
		}
	}

	// The fixture has exactly 54 readings (13 temp, 18 volt, 7 fan, 2 duty,
	// 5 curr, 9 pwr); a missing tag changes the count.
	if len(readings) != 54 {
		t.Errorf("got %d readings, want 54", len(readings))
	}
}

// TestSysClassify pins the <sys> classification table against the ids
// observed in a full live dump (2026-10-31, all sensors enabled). A rule
// regression silently re-buckets sys readings, so the table is exhaustive
// for the interesting families and spot-checks the unclassified ones.
func TestSysClassify(t *testing.T) {
	cases := []struct {
		id    string
		label string
		typ   Type
		unit  string
	}{
		// Clocks (MHz base).
		{"SCPUCLK", "CPU Clock", TypeClock, "MHz"},
		{"SGPU1CLK", "GPU1 Clock", TypeClock, "MHz"},
		{"SGPU1MEMCLK", "GPU1 Memory Clock", TypeClock, "MHz"},
		{"SMEMCLK", "Memory Clock", TypeClock, "MHz"},
		{"SNBCLK", "North Bridge Clock", TypeClock, "MHz"},
		{"SCC-1-1", "CPU Core #1 Clock", TypeClock, "MHz"},
		{"SCC-1-16", "CPU Core #16 Clock", TypeClock, "MHz"},
		// Refresh rate is Hz; FPS counters are not clocks.
		{"SAIDAFPS", "AIDA FPS", TypeOther, "FPS"},
		{"SRTSSFPS", "RTSS FPS", TypeOther, "FPS"},
		{"SVREFRATE", "Vertical Refresh Rate", TypeClock, "Hz"},
		// Utilization (label % rule is tested via the pwr category in TestRealDump).
		{"SCPUUTI", "CPU Utilization", TypeUsage, "%"},
		{"SCPU1UTI", "CPU1 Utilization", TypeUsage, "%"},
		{"SCPU32UTI", "CPU32 Utilization", TypeUsage, "%"},
		{"SGPU1UTI", "GPU1 Utilization", TypeUsage, "%"},
		{"SGPU1BIUTI", "GPU1 BI Utilization", TypeUsage, "%"},
		{"SGPU1MCUTI", "GPU1 MC Utilization", TypeUsage, "%"},
		{"SGPU1VEUTI", "GPU1 VE Utilization", TypeUsage, "%"},
		{"SMEMUTI", "Memory Utilization", TypeUsage, "%"},
		{"SVMEMUSAGE", "Video Memory Utilization", TypeUsage, "%"},
		{"SBATT", "Battery", TypeUsage, "%"},
		{"SMASTVOL", "Master Volume", TypeUsage, "%"},
		{"SDRVCUTI", "Drive C: Utilization", TypeUsage, "%"},
		// Data size (GB base).
		// Memory / virtual / video / GPU data volume is MB (AIDA64's own unit
		// column in the sensor log); only drive space is GB.
		{"SFREEMEM", "Free Memory", TypeDataSize, "MB"},
		{"SUSEDMEM", "Used Memory", TypeDataSize, "MB"},
		{"SFREEVIRTMEM", "Free Virtual Memory", TypeDataSize, "MB"},
		{"SUSEDVIRTMEM", "Used Virtual Memory", TypeDataSize, "MB"},
		{"SFREEVMEM", "Free Video Memory", TypeDataSize, "MB"},
		{"SUSEDVMEM", "Used Video Memory", TypeDataSize, "MB"},
		{"SGPU1USEDDEMEM", "GPU1 Used Dedicated Memory", TypeDataSize, "MB"},
		{"SGPU2USEDDYMEM", "GPU2 Used Dynamic Memory", TypeDataSize, "MB"},
		{"SDRVCUSEDSPC", "Drive C: Used Space", TypeDataSize, "GB"},
		{"SDRVGFREESPC", "Drive G: Free Space", TypeDataSize, "GB"},
		{"SNIC6TOTDL", "NIC6 Total Download", TypeDataSize, "MB"},
		{"SNIC6TOTUL", "NIC6 Total Upload", TypeDataSize, "MB"},
		// Throughput: only NIC connection speed is Mbps; NIC rates are KB/s,
		// disk read/write MB/s, memory bandwidth GB/s.
		{"SNIC6CONNSPD", "NIC6 Connection Speed", TypeRate, "Mbps"},
		{"SNIC6DLRATE", "NIC6 Download Rate", TypeRate, "KB/s"},
		{"SNIC6ULRATE", "NIC6 Upload Rate", TypeRate, "KB/s"},
		{"SMEMSPEED", "Memory Speed", TypeRate, "GB/s"},
		{"SDSK5READSPD", "Disk 5 Read Speed", TypeRate, "MB/s"},
		{"SDSK5WRITESPD", "Disk 5 Write Speed", TypeRate, "MB/s"},
		// Multipliers are dimensionless factors, unit x, type Other.
		{"SCPUMUL", "CPU Multiplier", TypeOther, "x"},
		{"SNBMUL", "North Bridge Multiplier", TypeOther, "x"},
		{"SCPUFSB", "CPU FSB", TypeClock, "MHz"},
		{"SDRAMFSB", "DRAM:FSB Ratio", TypeOther, "x"},
		// Intentionally unclassified: dates, names, addresses, status codes,
		// packed values. A future rule must not grab these by accident.
		{"SBIOSVER", "BIOS Version", TypeOther, ""},
		{"SYEAR", "Year", TypeOther, ""},
		{"SMONTHNAME", "Month Name", TypeOther, ""},
		{"SMOBONAME", "Motherboard Name", TypeOther, ""},
		{"SEXTIPADDR", "External IP Address", TypeOther, ""},
		{"SDESKRES", "Desktop Resolution", TypeOther, ""},
		{"SPROCESSES", "Processes", TypeOther, ""},
		{"SGPU1PERFCAP", "GPU1 PerfCap Reason", TypeOther, ""},
		{"SSLISTA", "SLI Status", TypeOther, ""},
		{"SDSK1ACT", "Disk 1 Activity", TypeUsage, "%"},
		{"SUPTIME", "UpTime", TypeOther, ""},
	}
	for _, c := range cases {
		got := Classify(CategorySys, c.id, c.label)
		if got != c.typ {
			t.Errorf("Classify(sys,%s,%q) type = %v, want %v", c.id, c.label, got, c.typ)
			continue
		}
		u := Reading{ID: c.id, Label: c.label, Category: CategorySys, Type: got}.Unit()
		if u != c.unit {
			t.Errorf("%s unit = %q, want %q", c.id, u, c.unit)
		}
	}
}

// TestFullDump parses the full-capture fixture (all AIDA64 shared-memory
// sensors enabled on a real PC, 2026-10-31) and pins the classification
// coverage: every non-sys reading carries its unit, and the sys breakdown
// matches the sysRule table. A rule that silently re-buckets a family (e.g.
// drops the SCC- clock family into Other) changes these counts.
func TestFullDump(t *testing.T) {
	buf, err := os.ReadFile("testdata/full_dump.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	readings, err := DecodeBytes(buf)
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}
	if len(readings) != 222 {
		t.Fatalf("got %d readings, want 222", len(readings))
	}

	byCat := map[Category]int{}
	unitless := map[Category]int{}
	byType := map[Type]int{}
	for _, r := range readings {
		byCat[r.Category]++
		byType[r.Type]++
		if r.Unit() == "" {
			unitless[r.Category]++
		}
	}

	wantCat := map[Category]int{"temp": 16, "volt": 17, "fan": 7, "duty": 2, "curr": 5, "pwr": 9, "sys": 166}
	for c, n := range wantCat {
		if byCat[c] != n {
			t.Errorf("category %s: got %d, want %d", c, byCat[c], n)
		}
	}

	// Non-sys categories: every reading must have a unit.
	for c := range wantCat {
		if c == CategorySys {
			continue
		}
		if n := unitless[c]; n != 0 {
			t.Errorf("category %s: %d readings without a unit", c, n)
		}
	}

	// The sys breakdown pins the rule table: 132 of 166 sys readings classify
	// (52 usage incl. per-disk activity, 23 clock, 25 rate, 32 data size), the
	// 34 remaining are dates/names/addresses/status codes plus the multiplier
	// "x" and FPS counters, intentionally Other. A rule that re-buckets a
	// family changes these counts.
	sysOnly := map[Type]int{}
	for _, r := range readings {
		if r.Category == CategorySys {
			sysOnly[r.Type]++
		}
	}
	wantSys := map[Type]int{TypeUsage: 52, TypeClock: 23, TypeRate: 25, TypeDataSize: 32, TypeOther: 34}
	for ty, n := range wantSys {
		if sysOnly[ty] != n {
			t.Errorf("sys type %v: got %d, want %d", ty, sysOnly[ty], n)
		}
	}
	// Global type counts, including the non-sys percentages (duty 2 + pwr 1
	// label-%) and the 8 pwr-watt readings left after PGPU1TDPP.
	wantGlobal := map[Type]int{TypeTemp: 16, TypeVolt: 17, TypeFan: 7, TypeCurrent: 5, TypePower: 8, TypeClock: 23, TypeUsage: 55, TypeOther: 34, TypeRate: 25, TypeDataSize: 32}
	for ty, n := range wantGlobal {
		if byType[ty] != n {
			t.Errorf("global type %v: got %d, want %d", ty, byType[ty], n)
		}
	}
}

func TestParseValue(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"52.5", 52.5, true},
		{"1.164", 1.164, true},
		{"42", 42, true},
		{"5,5", 5.5, true},
		{"", 0, false},
		{"N/A", 0, false},
	}
	for _, c := range cases {
		got, err := parseValue(c.in)
		if c.ok && err != nil {
			t.Errorf("parseValue(%q): unexpected err %v", c.in, err)
		}
		if !c.ok && err == nil {
			t.Errorf("parseValue(%q): expected err", c.in)
		}
		if c.ok && got != c.want {
			t.Errorf("parseValue(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
