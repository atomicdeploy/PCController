package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/control"
)

type menuPage struct {
	ID          byte
	Key         string
	Name        string
	Short       string
	Description string
	Category    string
}

type menuConfigurationEntry struct {
	Page    menuPage
	Rank    int
	Visible bool
}

func menuPagesFromInfo(values []control.MenuPageInfo) []menuPage {
	result := make([]menuPage, 0, len(values))
	for _, value := range values {
		result = append(result, menuPage{
			ID: value.ID, Key: value.Key, Name: value.Name,
			Short: strings.TrimSpace(value.Label), Description: value.Description,
			Category: menuCategory(value.Key),
		})
	}
	return result
}

func menuPagesForCapabilities(capabilities uint32) []menuPage {
	return menuPagesFromInfo(control.MenuPagesForCapabilities(capabilities))
}

func (model Model) activeMenuPages() []menuPage {
	if len(model.menuPages) != 0 {
		return model.menuPages
	}
	return menuPagesForCapabilities(model.snapshot().Hello.Capabilities)
}

func (model Model) menuPageByID(id byte) menuPage {
	for _, page := range model.activeMenuPages() {
		if page.ID == id {
			return page
		}
	}
	return menuPage{
		ID: id, Key: fmt.Sprintf("page-%d", id), Name: "Unknown page",
		Short: "?", Description: "Not in the active board menu catalog", Category: "Unknown",
	}
}

func (model Model) statusMenuID() (byte, bool) {
	for _, page := range model.activeMenuPages() {
		if page.Key == "status" {
			return page.ID, true
		}
	}
	return 0, false
}

func (model *Model) applyMenuCatalog(catalog control.MenuCatalog) {
	model.menuPages = menuPagesFromInfo(catalog.Pages)
	model.menuLayout = cloneMenuLayout(catalog.Layout)
	model.menuLayoutOriginal = cloneMenuLayout(catalog.Layout)
	model.menuLayoutStaged = cloneMenuLayout(catalog.Layout)
	model.menuLayoutDirty = false
	model.menuLayoutError = ""
	model.menuCatalogSource = catalog.Source
	model.menuCatalogHash = catalog.FirmwareHash
	model.menuCatalogLoaded = true
}

func menuCategory(key string) string {
	switch key {
	case "status", "voltage", "current", "temperature-led", "temperature-bt":
		return "Monitoring"
	case "illumination", "bt-audio", "settings":
		return "Environment"
	case "pwm-test", "relay-test", "user-pwm", "user-relays", "motion":
		return "Outputs"
	case "keys", "rf-learn":
		return "Inputs / RF"
	default:
		return "Other"
	}
}

func cloneMenuLayout(layout control.MenuLayout) control.MenuLayout {
	layout.Order = append([]byte(nil), layout.Order...)
	return layout
}

func (model Model) menuConfigurationEntries() []menuConfigurationEntry {
	layout := model.menuLayoutStaged
	if len(layout.Order) == 0 {
		values := make([]control.MenuPageInfo, 0, len(model.activeMenuPages()))
		for _, page := range model.activeMenuPages() {
			values = append(values, control.MenuPageInfo{
				ID: page.ID, Key: page.Key, Label: page.Short,
				Name: page.Name, Description: page.Description,
			})
		}
		layout, _ = control.DefaultMenuLayout(values)
	}
	byID := make(map[byte]menuPage, len(model.activeMenuPages()))
	for _, page := range model.activeMenuPages() {
		byID[page.ID] = page
	}
	entries := make([]menuConfigurationEntry, 0, len(layout.Order))
	query := strings.ToLower(strings.TrimSpace(model.menuLayoutSearch))
	for rank, id := range layout.Order {
		page, ok := byID[id]
		if !ok {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				strconv.Itoa(int(id)), page.Key, page.Short, page.Name,
				page.Description, page.Category,
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		entries = append(entries, menuConfigurationEntry{
			Page: page, Rank: rank, Visible: layout.Visible(id),
		})
	}
	switch model.menuLayoutSort {
	case "name":
		sort.SliceStable(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].Page.Name) < strings.ToLower(entries[j].Page.Name)
		})
	case "visibility":
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Visible != entries[j].Visible {
				return entries[i].Visible
			}
			return entries[i].Rank < entries[j].Rank
		})
	case "category":
		sort.SliceStable(entries, func(i, j int) bool {
			left := strings.ToLower(entries[i].Page.Category + "\x00" + entries[i].Page.Name)
			right := strings.ToLower(entries[j].Page.Category + "\x00" + entries[j].Page.Name)
			return left < right
		})
	}
	return entries
}

func (model Model) selectedMenuConfiguration() (menuConfigurationEntry, bool) {
	entries := model.menuConfigurationEntries()
	if len(entries) == 0 {
		return menuConfigurationEntry{}, false
	}
	index := model.cursor
	if index < 0 {
		index = 0
	}
	if index >= len(entries) {
		index = len(entries) - 1
	}
	return entries[index], true
}

func (model Model) visibleMenuPages() []menuPage {
	entries := model.menuConfigurationEntriesByRank()
	result := make([]menuPage, 0, len(entries))
	for _, entry := range entries {
		if entry.Visible {
			result = append(result, entry.Page)
		}
	}
	return result
}

func (model Model) menuConfigurationEntriesByRank() []menuConfigurationEntry {
	copyModel := model
	copyModel.menuLayoutSearch = ""
	copyModel.menuLayoutSort = "rank"
	return copyModel.menuConfigurationEntries()
}
