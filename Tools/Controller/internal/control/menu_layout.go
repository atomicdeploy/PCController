package control

import (
	"errors"
	"fmt"
)

const MenuLayoutSchema byte = 2

var ErrMenuLayoutUnsupported = errors.New("connected firmware does not advertise persistent menu layout support")

// MenuLayout describes the board-owned menu order and visibility. Order is a
// complete permutation of stable page IDs; each ID's bit in VisibleMask
// controls whether local Previous/Next navigation visits it.
type MenuLayout struct {
	Schema      byte   `json:"schema"`
	Supported   bool   `json:"supported"`
	Persistent  bool   `json:"persistent"`
	Source      string `json:"source"`
	VisibleMask uint16 `json:"visible_mask"`
	Order       []byte `json:"order"`
}

// DefaultMenuLayout preserves catalog order and makes every browsable page
// visible. Retired wire aliases remain in the complete order but are never
// locally visible. It is the truthful read-only fallback for older firmware.
func DefaultMenuLayout(pages []MenuPageInfo) (MenuLayout, error) {
	layout := MenuLayout{
		Schema: MenuLayoutSchema,
		Source: "catalog order (persistent layout capability unavailable)",
		Order:  make([]byte, len(pages)),
	}
	for index, page := range pages {
		if page.ID >= 16 {
			return MenuLayout{}, fmt.Errorf("menu page ID %d cannot be represented by the 16-bit visibility mask", page.ID)
		}
		layout.Order[index] = page.ID
		if _, retired := retiredMenuAliasTarget(pages, page.ID); !retired {
			layout.VisibleMask |= uint16(1) << page.ID
		}
	}
	if _, err := CanonicalMenuLayout(pages, layout); err != nil {
		return MenuLayout{}, err
	}
	return layout, nil
}

// CanonicalMenuLayout validates a complete permutation and returns an owned
// copy safe for storage, mutation, and API responses.
func CanonicalMenuLayout(pages []MenuPageInfo, layout MenuLayout) (MenuLayout, error) {
	if layout.Schema != 0 && layout.Schema != 1 && layout.Schema != MenuLayoutSchema {
		return MenuLayout{}, fmt.Errorf("unsupported menu-layout schema %d", layout.Schema)
	}
	if len(pages) == 0 {
		return MenuLayout{}, errors.New("menu catalog is empty")
	}
	if len(layout.Order) != len(pages) {
		return MenuLayout{}, fmt.Errorf("menu order contains %d IDs, need complete permutation of %d", len(layout.Order), len(pages))
	}
	expected := make(map[byte]bool, len(pages))
	var allowedMask uint16
	for _, page := range pages {
		if page.ID >= 16 {
			return MenuLayout{}, fmt.Errorf("menu page ID %d cannot be represented by the 16-bit visibility mask", page.ID)
		}
		if expected[page.ID] {
			return MenuLayout{}, fmt.Errorf("menu catalog repeats stable page ID %d", page.ID)
		}
		expected[page.ID] = true
		allowedMask |= uint16(1) << page.ID
	}
	seen := make(map[byte]bool, len(layout.Order))
	for rank, id := range layout.Order {
		if !expected[id] {
			return MenuLayout{}, fmt.Errorf("menu order rank %d contains unknown page ID %d", rank, id)
		}
		if seen[id] {
			return MenuLayout{}, fmt.Errorf("menu order repeats page ID %d", id)
		}
		seen[id] = true
	}
	if extra := layout.VisibleMask &^ allowedMask; extra != 0 {
		return MenuLayout{}, fmt.Errorf("menu visibility mask sets out-of-catalog bits 0x%04X", extra)
	}
	layout.Order = append([]byte(nil), layout.Order...)
	layout = normalizeRetiredMenuAliases(pages, layout)
	if layout.VisibleMask&allowedMask == 0 {
		return MenuLayout{}, errors.New("menu visibility mask must leave at least one page visible")
	}
	layout.Schema = MenuLayoutSchema
	return layout, nil
}

func (layout MenuLayout) Visible(id byte) bool {
	return id < 16 && layout.VisibleMask&(uint16(1)<<id) != 0
}

func (layout MenuLayout) Rank(id byte) (int, bool) {
	for rank, candidate := range layout.Order {
		if candidate == id {
			return rank, true
		}
	}
	return 0, false
}

// OrderedMenuPages applies stable-ID ranks without changing the page IDs used
// by direct navigation and protocol calls.
func OrderedMenuPages(pages []MenuPageInfo, layout MenuLayout) ([]MenuPageInfo, error) {
	layout, err := CanonicalMenuLayout(pages, layout)
	if err != nil {
		return nil, err
	}
	byID := make(map[byte]MenuPageInfo, len(pages))
	for _, page := range pages {
		byID[page.ID] = page
	}
	result := make([]MenuPageInfo, 0, len(pages))
	for _, id := range layout.Order {
		result = append(result, byID[id])
	}
	return result, nil
}

// MoveMenuPage returns a validated layout with one stable ID moved to the
// requested zero-based rank.
func MoveMenuPage(pages []MenuPageInfo, layout MenuLayout, id byte, rank int) (MenuLayout, error) {
	if _, retired := retiredMenuAliasTarget(pages, id); retired {
		return MenuLayout{}, fmt.Errorf("menu page ID %d is a retired KEY alias and cannot be reordered", id)
	}
	layout, err := CanonicalMenuLayout(pages, layout)
	if err != nil {
		return MenuLayout{}, err
	}
	current, ok := layout.Rank(id)
	if !ok {
		return MenuLayout{}, fmt.Errorf("menu page ID %d is not in the active layout", id)
	}
	if rank < 0 || rank >= len(layout.Order) {
		return MenuLayout{}, fmt.Errorf("menu rank must be 0..%d", len(layout.Order)-1)
	}
	if current == rank {
		return layout, nil
	}
	copy(layout.Order[current:], layout.Order[current+1:])
	layout.Order = layout.Order[:len(layout.Order)-1]
	layout.Order = append(layout.Order, 0)
	copy(layout.Order[rank+1:], layout.Order[rank:])
	layout.Order[rank] = id
	return CanonicalMenuLayout(pages, layout)
}

func SetMenuPageVisible(pages []MenuPageInfo, layout MenuLayout, id byte, visible bool) (MenuLayout, error) {
	if _, retired := retiredMenuAliasTarget(pages, id); retired {
		return MenuLayout{}, fmt.Errorf("menu page ID %d is a retired KEY alias and cannot be shown or hidden", id)
	}
	layout, err := CanonicalMenuLayout(pages, layout)
	if err != nil {
		return MenuLayout{}, err
	}
	if _, ok := layout.Rank(id); !ok {
		return MenuLayout{}, fmt.Errorf("menu page ID %d is not in the active layout", id)
	}
	bit := uint16(1) << id
	if visible {
		layout.VisibleMask |= bit
	} else {
		layout.VisibleMask &^= bit
	}
	return CanonicalMenuLayout(pages, layout)
}

// retiredMenuAliasTarget identifies aliases only in the current production
// catalog. Historical firmware retains a genuine MOVE page at the same ID,
// and its compatibility catalog deliberately keeps a different description.
func retiredMenuAliasTarget(pages []MenuPageInfo, id byte) (byte, bool) {
	if id != menuPageMotionAlias {
		return 0, false
	}
	for _, page := range pages {
		if page.ID == id && page.Description == retiredMotionAliasDetails {
			return menuPageKeys, true
		}
	}
	return 0, false
}

// normalizeRetiredMenuAliases accepts legacy persisted masks without allowing
// ID 12 to become a second local KEY page. When an old layout showed only
// MOVE, swapping IDs 9 and 12 preserves the user's effective rank while RF
// and every other stable page retain their order.
func normalizeRetiredMenuAliases(pages []MenuPageInfo, layout MenuLayout) MenuLayout {
	for _, page := range pages {
		target, retired := retiredMenuAliasTarget(pages, page.ID)
		if !retired {
			continue
		}
		aliasBit := uint16(1) << page.ID
		if layout.VisibleMask&aliasBit == 0 {
			continue
		}
		targetBit := uint16(1) << target
		targetWasVisible := layout.VisibleMask&targetBit != 0
		layout.VisibleMask = layout.VisibleMask&^aliasBit | targetBit
		if !targetWasVisible {
			for rank, id := range layout.Order {
				switch id {
				case page.ID:
					layout.Order[rank] = target
				case target:
					layout.Order[rank] = page.ID
				}
			}
		}
	}
	return layout
}
