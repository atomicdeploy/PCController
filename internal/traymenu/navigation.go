package traymenu

import (
	"strings"
	"unicode"
)

type navigator struct {
	items    []Item
	selected int
}

func newNavigator(items []Item) navigator {
	n := navigator{items: items, selected: -1}
	n.selected = n.first()
	return n
}

func (n navigator) selectable(index int) bool {
	return index >= 0 && index < len(n.items) &&
		!n.items[index].Separator && !n.items[index].Disabled
}

func (n navigator) first() int {
	for index := range n.items {
		if n.selectable(index) {
			return index
		}
	}
	return -1
}

func (n navigator) last() int {
	for index := len(n.items) - 1; index >= 0; index-- {
		if n.selectable(index) {
			return index
		}
	}
	return -1
}

func (n *navigator) move(delta int) bool {
	if len(n.items) == 0 || delta == 0 {
		return false
	}
	old := n.selected
	index := n.selected
	if index < 0 {
		if delta > 0 {
			n.selected = n.first()
		} else {
			n.selected = n.last()
		}
		return old != n.selected
	}
	for steps := 0; steps < len(n.items); steps++ {
		index = (index + delta + len(n.items)) % len(n.items)
		if n.selectable(index) {
			n.selected = index
			return old != n.selected
		}
	}
	return false
}

func (n *navigator) home() bool {
	old := n.selected
	n.selected = n.first()
	return old != n.selected
}

func (n *navigator) end() bool {
	old := n.selected
	n.selected = n.last()
	return old != n.selected
}

func (n *navigator) selectIndex(index int) bool {
	old := n.selected
	if n.selectable(index) {
		n.selected = index
	} else {
		n.selected = -1
	}
	return old != n.selected
}

func (n *navigator) matchRune(target rune) bool {
	target = unicode.ToLower(target)
	if unicode.IsSpace(target) || unicode.IsControl(target) {
		return false
	}
	start := n.selected + 1
	for offset := 0; offset < len(n.items); offset++ {
		index := (start + offset) % len(n.items)
		if !n.selectable(index) {
			continue
		}
		label := strings.TrimSpace(strings.ReplaceAll(n.items[index].Label, "&", ""))
		first, _ := firstRune(label)
		if unicode.ToLower(first) == target {
			n.selected = index
			return true
		}
	}
	return false
}

func firstRune(value string) (rune, int) {
	for index, r := range value {
		return r, index
	}
	return 0, -1
}

func (n navigator) action() (ActionID, bool) {
	if !n.selectable(n.selected) {
		return "", false
	}
	return n.items[n.selected].ID, true
}
