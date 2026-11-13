package aida64

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxBuf bounds how much of the shared-memory block we read. AIDA64 writes a
// NUL-terminated string of tags; the documented block is 10 KB and a typical
// payload is 1-3 KB, so 64 KB is a safe upper bound.
const maxBuf = 64 * 1024

// readingTag is one <temp>/<volt>/<fan>/<duty>/<pwr>/<sys> element. The
// shared-memory content is a flat run of such elements with no root, so we
// decode them one by one (see DecodeBytes).
type readingTag struct {
	XMLName xml.Name
	ID      string `xml:"id"`
	Label   string `xml:"label"`
	Value   string `xml:"value"`
}

// knownTags is the set of top-level reading tags AIDA64 emits. Unknown tags
// are skipped rather than erroring so a future AIDA64 version adding a new
// category does not break parsing.
var knownTags = map[string]Category{
	"temp": CategoryTemp,
	"volt": CategoryVolt,
	"fan":  CategoryFan,
	"duty": CategoryDuty,
	"curr": CategoryCurr,
	"pwr":  CategoryPwr,
	"sys":  CategorySys,
}

// DecodeBytes parses a shared-memory block (NUL-terminated or not) into
// readings. It tolerates the block having no root element, which is how
// AIDA64 actually writes it.
func DecodeBytes(buf []byte) ([]Reading, error) {
	if i := indexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	if len(buf) == 0 {
		return nil, nil
	}

	var (
		out  []Reading
		dec  = xml.NewDecoder(strings.NewReader(string(buf)))
		seen = map[string]bool{}
	)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("aida64: parse error: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		cat, ok := knownTags[se.Name.Local]
		if !ok {
			if skipErr := dec.Skip(); skipErr != nil {
				return nil, fmt.Errorf("aida64: skip %q: %w", se.Name.Local, skipErr)
			}
			continue
		}

		var rt readingTag
		if err := dec.DecodeElement(&rt, &se); err != nil {
			return nil, fmt.Errorf("aida64: decode <%s>: %w", se.Name.Local, err)
		}

		r := Reading{
			ID:       rt.ID,
			Label:    rt.Label,
			Category: cat,
			Type:     Classify(cat, rt.ID, rt.Label),
			RawValue: strings.TrimSpace(rt.Value),
		}
		r.Value, err = parseValue(r.RawValue)
		if err != nil {
			// Keep the reading; the plugin can still show the raw string.
			r.Value = 0
		}
		// Duplicate ids (two fans with the same label, etc.) are possible in
		// real dumps; make the key unique so downstream maps stay 1:1.
		key := r.ID
		for seen[key] {
			key += "#" + strconv.Itoa(len(out)+1)
		}
		seen[key] = true
		out = append(out, r)
	}

	return out, nil
}

// DecodeString is DecodeBytes for string input (tests, registry fallback).
func DecodeString(s string) ([]Reading, error) {
	return DecodeBytes([]byte(s))
}

// parseValue converts AIDA64's value string to float. Values are locale
// formatted; AIDA64 shared memory uses a period decimal separator, but we
// accept a comma as a fallback for older builds.
func parseValue(raw string) (float64, error) {
	v := strings.ReplaceAll(raw, ",", ".")
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, fmt.Errorf("empty value")
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}

func indexByte(buf []byte, b byte) int {
	for i, c := range buf {
		if c == b {
			return i
		}
	}
	return -1
}
