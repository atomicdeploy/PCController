//go:build windows

package ui

import (
	"pccontroller.local/desktop/presentation"
	"strconv"
	"time"
)

// Counters belong to the open dialog, not a catalog refresh or a table row.
// Cumulative packet counts preserve repeats even when notifications coalesce.
type controllerRFSession struct {
	owner       string
	initialized bool
	baseline    map[string]uint64
}

func (session *controllerRFSession) decorate(page *presentation.Page, now time.Time) bool {
	if !session.initialized || session.owner != page.Owner {
		session.owner = page.Owner
		session.initialized = true
		session.baseline = map[string]uint64{}
		for _, receipt := range page.Receipts {
			session.baseline[receipt.Identity] = receipt.Count
		}
	}
	changed := false
	for _, row := range page.Rows {
		if len(row) < 2 {
			continue
		}
		receipt, ok := page.Receipts[row[0]]
		if !ok {
			continue
		}
		base := session.baseline[receipt.Identity]
		if receipt.Count < base {
			base = receipt.Count
			session.baseline[receipt.Identity] = base
		}
		count := strconv.FormatUint(receipt.Count-base, 10)
		indicator := "○"
		age := now.Sub(receipt.LastSeen)
		if receipt.Connected && !receipt.LastSeen.IsZero() && age >= 0 && age < 250*time.Millisecond {
			indicator = "●"
		}
		if row[len(row)-2] != indicator || row[len(row)-1] != count {
			changed = true
			row[len(row)-2], row[len(row)-1] = indicator, count
		}
	}
	return changed
}
