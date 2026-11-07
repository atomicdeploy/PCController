//go:build windows

package ui

import (
	"pccontroller.local/desktop/presentation"
	"testing"
	"time"
)

func TestControllerRFCountsAreModalLocalAndPresenceExpiresWithoutPolling(t *testing.T) {
	now := time.Unix(100, 0)
	session := controllerRFSession{}
	page := presentation.Page{Owner: "host:1", Rows: [][]string{{"known", "○", "0"}}, Receipts: map[string]presentation.RFReceipt{"known": {Identity: "42:24:1", Count: 20, LastSeen: now, Learned: true, Connected: true}}}
	session.decorate(&page, now)
	if page.Rows[0][1] != "●" || page.Rows[0][2] != "0" {
		t.Fatal(page.Rows)
	}
	receipt := page.Receipts["known"]
	receipt.Count = 27
	receipt.LastSeen = now.Add(20 * time.Millisecond)
	page.Receipts["known"] = receipt
	session.decorate(&page, now.Add(30*time.Millisecond))
	if page.Rows[0][2] != "7" {
		t.Fatal("refresh reset cumulative count", page.Rows)
	}
	session.decorate(&page, now.Add(270*time.Millisecond))
	if page.Rows[0][1] != "○" || page.Rows[0][2] != "7" {
		t.Fatal("quiet indicator did not expire independently", page.Rows)
	}
	page.Owner = "host:2"
	session.decorate(&page, now)
	if page.Rows[0][2] != "0" {
		t.Fatal("owner replacement retained previous counter")
	}
	page.Rows = append(page.Rows, []string{"new", "○", "0"})
	page.Receipts["new"] = presentation.RFReceipt{Identity: "42:32:1", Count: 3, Connected: true, LastSeen: now}
	session.decorate(&page, now)
	if page.Rows[1][2] != "3" {
		t.Fatal("new complete waveform identity was collapsed", page.Rows)
	}
	receipt = page.Receipts["known"]
	receipt.Connected = false
	page.Receipts["known"] = receipt
	session.decorate(&page, now)
	if page.Rows[0][1] != "○" {
		t.Fatal("disconnect retained activity")
	}
	if controllerFormActionSlot("rf", "learn") != controllerFormActionSlot("rf", "cancel") {
		t.Fatal("learning lifecycle moves between unrelated button slots")
	}
}
