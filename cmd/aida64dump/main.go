//go:build windows

// Command aida64dump reads the AIDA64_SensorValues shared-memory block and
// prints the parsed readings. It exists so the parser can be validated
// against a live AIDA64 before the gRPC bridge and Stream Deck plugin are
// built on top of it.
//
// Usage:
//
//	aida64dump.exe            # table of cat/id/label/value+unit
//	aida64dump.exe -raw       # raw shared-memory string
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/poizenjam/aida64sd/internal/aida64"
)

func main() {
	raw := flag.Bool("raw", false, "print the raw shared-memory string")
	flag.Parse()

	buf, err := aida64.ReadBlock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aida64dump: %v\n", err)
		fmt.Fprintln(os.Stderr, "aida64dump: is AIDA64 running with File > Preferences > Hardware Monitoring > External Applications > Enable shared memory checked?")
		os.Exit(1)
	}

	if *raw {
		os.Stdout.Write(buf)
		return
	}

	readings, err := aida64.DecodeBytes(buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aida64dump: parse: %v\n", err)
		os.Exit(1)
	}
	sort.Slice(readings, func(i, j int) bool {
		if readings[i].Category != readings[j].Category {
			return readings[i].Category < readings[j].Category
		}
		return readings[i].ID < readings[j].ID
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tID\tLABEL\tVALUE")
	for _, r := range readings {
		unit := r.Type.UnitFor()
		val := fmt.Sprintf("%.2f", r.Value)
		if unit != "" {
			val += " " + unit
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Category, r.ID, r.Label, val)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "aida64dump:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "# %d readings\n", len(readings))
}
