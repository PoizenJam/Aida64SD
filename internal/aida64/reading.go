package aida64

import (
	"regexp"
	"strings"
)

// Category is the top-level AIDA64 shared-memory tag. AIDA64 writes one tag
// per reading, grouped by category; the tag name carries the physical type.
type Category string

const (
	CategoryTemp Category = "temp" // temperatures, always Celsius
	CategoryVolt Category = "volt" // voltages, V
	CategoryFan  Category = "fan"  // fan speeds, RPM
	CategoryDuty Category = "duty" // fan duty cycle, %
	CategoryCurr Category = "curr" // currents, A
	CategoryPwr  Category = "pwr"  // power, W
	CategorySys  Category = "sys"  // miscellaneous system values
)

// Type is the normalized reading type used downstream by the plugin. The
// first nine mirror the HWiNFO plugin's ReadingType set so tiles and
// thresholds behave identically across the two backends; TypeRate and
// TypeDataSize are AIDA64 additions for the throughput / data-size readings
// that only AIDA64's <sys> category produces. The numeric values are sent
// over gRPC as Reading.typeI and must stay aligned with the host switches on
// them (handlers.go getDefaultMinMaxForReading, threshold_state.go).
type Type int

const (
	TypeNone Type = iota
	TypeTemp
	TypeVolt
	TypeFan
	TypeCurrent
	TypePower
	TypeClock
	TypeUsage
	TypeOther
	TypeRate     // throughput or link speed
	TypeDataSize // memory / disk / network data volume
)

func (t Type) String() string {
	return [...]string{"None", "Temp", "Volt", "Fan", "Current", "Power", "Clock", "Usage", "Other", "Rate", "DataSize"}[t]
}

// UnitFor returns the base display unit for a reading of the given type. A
// sys reading may refine this to a more specific unit (MB/s disk speed, Hz
// refresh rate) via Reading.Unit().
func (t Type) UnitFor() string {
	switch t {
	case TypeTemp:
		return "C"
	case TypeVolt:
		return "V"
	case TypeFan:
		return "RPM"
	case TypeCurrent:
		return "A"
	case TypePower:
		return "W"
	case TypeClock:
		return "MHz"
	case TypeUsage:
		return "%"
	case TypeRate:
		return "Mbps"
	case TypeDataSize:
		return "GB"
	default:
		return ""
	}
}

// Reading is one parsed sensor value from AIDA64 shared memory.
type Reading struct {
	// ID is the AIDA64 reading id (e.g. "TCPU", "SCPUCLK"). It is stable
	// across relaunches and is the primary picker key.
	ID string
	// Label is AIDA64's English label for the reading (e.g. "CPU Package").
	Label string
	// Category is the originating tag (temp/volt/fan/duty/pwr/sys).
	Category Category
	// Type is the normalized type derived from Category + id + label.
	Type Type
	// Value is the numeric value. Temperatures are always Celsius.
	Value float64
	// RawValue is the value string exactly as AIDA64 wrote it, preserved
	// for diagnostics and for readings whose value is not a plain float.
	RawValue string
}

// CategoryType maps a category tag to its default reading type.
func CategoryType(c Category) Type {
	switch c {
	case CategoryTemp:
		return TypeTemp
	case CategoryVolt:
		return TypeVolt
	case CategoryFan:
		return TypeFan
	case CategoryDuty:
		return TypeUsage
	case CategoryCurr:
		return TypeCurrent
	case CategoryPwr:
		return TypePower
	default:
		return TypeOther
	}
}

// sysRule classifies one <sys> reading by id. AIDA64 packs clocks,
// utilization, memory, storage, network and status counters into a single
// sys tag with no unit field, so the id (a stable AIDA64 key) is the
// classifier. First matching rule wins; the list is ordered most-specific
// first so a compound id (SGPU1BIUTI vs SGPU1CLK) hits the right entry.
// unit is the display-unit override; "" means use the type's UnitFor().
type sysRule struct {
	pat  *regexp.Regexp
	typ  Type
	unit string
}

// sysRules classify <sys> ids by unit, verified against AIDA64's own
// sensor-log (CSV) unit column on a live box (Ryzen 9950X3D, dual GPU).
var sysRules = []sysRule{
	{regexp.MustCompile(`^SCCU-.+$`), TypeClock, ""},
	// Throughput. NIC *connection* speed is the only Mbps reading; NIC
	// download/upload rates are KB/s, disk read/write are MB/s, memory
	// bandwidth GB/s.
	{regexp.MustCompile(`^SNIC[0-9]+CONNSPD$`), TypeRate, "Mbps"},
	{regexp.MustCompile(`^SNIC[0-9]+(DLRATE|ULRATE)$`), TypeRate, "KB/s"},
	{regexp.MustCompile(`^SDSK[0-9]+(READ|WRITE)SPD$`), TypeRate, "MB/s"},
	{regexp.MustCompile(`^SMEMSPEED$`), TypeRate, "GB/s"},
	// Data volume. Memory / virtual / video / GPU memory and NIC session
	// totals are MB; drive used/free space is GB.
	{regexp.MustCompile(`^SNIC[0-9]+TOT(UL|DL)$`), TypeDataSize, "MB"},
	{regexp.MustCompile(`^S(USED|FREE)(VIRT)?MEM$|^S(USED|FREE)VMEM$|^SGPU[0-9]+USED(DE|DY)MEM$`), TypeDataSize, "MB"},
	{regexp.MustCompile(`^SDRV[A-Z]?(USED|FREE)SPC$`), TypeDataSize, "GB"},
	// Percentage / load, including per-disk activity.
	{regexp.MustCompile(`^S[A-Z0-9]*UTI$`), TypeUsage, ""},
	{regexp.MustCompile(`^SDSK[0-9]+ACT$`), TypeUsage, ""},
	{regexp.MustCompile(`^SVMEMUSAGE$|^SBATT$|^SMASTVOL$`), TypeUsage, ""},
	// Clocks, base unit MHz (the CPU FSB is a real frequency, not a ratio).
	{regexp.MustCompile(`^S(GPU[0-9]+)?CLK$|^SMEMCLK$|^SNBCLK$|^SCPUFSB$`), TypeClock, ""},
	{regexp.MustCompile(`^SVREFRATE$`), TypeClock, "Hz"},
	// Frame counters are not clocks; FPS is their own unit.
	{regexp.MustCompile(`^S[A-Z0-9]*FPS$`), TypeOther, "FPS"},
	// Multiplicative factors (multiplier / FSB ratio), unit x.
	{regexp.MustCompile(`^SCPUMUL$|^SNBMUL$|^SDRAMFSB$`), TypeOther, "x"},
}

// Classify derives the normalized type for a reading. The category tag is
// the primary signal; the id (and, for sys, the label) refine it because
// AIDA64 has no per-reading unit field (unlike HWiNFO's tReading enum).
// Priority:
//  1. a label ending in '%' is a percentage even inside a non-duty category
//     (observed: <pwr> PGPU1TDPP "GPU1 TDP%");
//  2. the category default (temp/volt/fan/duty/curr/pwr);
//  3. for sys, the first matching sysRule id, else a label fallback, else
//     TypeOther.
//
// sysClassify returns the type and display-unit override (empty = use the
// type's base unit) for a sys reading id, and whether a rule matched.
func sysClassify(id string) (Type, string, bool) {
	for _, r := range sysRules {
		if r.pat.MatchString(id) {
			return r.typ, r.unit, true
		}
	}
	return TypeOther, "", false
}

func Classify(cat Category, id, label string) Type {
	if strings.HasSuffix(strings.TrimSpace(label), "%") {
		return TypeUsage
	}
	switch cat {
	case CategoryTemp:
		return TypeTemp
	case CategoryVolt:
		return TypeVolt
	case CategoryFan:
		return TypeFan
	case CategoryDuty:
		return TypeUsage
	case CategoryCurr:
		return TypeCurrent
	case CategoryPwr:
		return TypePower
	case CategorySys:
		if t, _, ok := sysClassify(id); ok {
			return t
		}
		// Label fallback for sys ids not yet enumerated; keeps a future
		// AIDA64 clock/usage reading from landing in the untyped bucket.
		lower := strings.ToLower(label)
		switch {
		case strings.Contains(lower, "clock"):
			return TypeClock
		case strings.Contains(lower, "utilization") || strings.Contains(lower, "usage"):
			return TypeUsage
		}
		return TypeOther
	default:
		return TypeOther
	}
}

// Unit returns the display unit for this reading: the sys unit override
// (MB/s, GB/s, Hz, x) when the id matched a rule with one, else the type's
// base unit, else "" for TypeOther.
func (r Reading) Unit() string {
	if r.Category == CategorySys {
		if _, unit, ok := sysClassify(r.ID); ok && unit != "" {
			return unit
		}
	}
	return r.Type.UnitFor()
}
