package appconfig

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// ChannelFolder belongs to one presentation section of one board profile.
// It never changes a channel's wiring, role, lock, visibility or output value.
type ChannelFolder struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type ChannelFolderMutation struct {
	Operation string   `json:"operation"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	NextName  *string  `json:"next_name,omitempty"`
	Icon      *string  `json:"icon,omitempty"`
	Keys      []string `json:"keys,omitempty"`
}

func ChannelFolderKind(kind string) string {
	switch kind {
	case "seat", "side", "motion":
		return "motion"
	case "relay":
		return "relay"
	case "mosfet", "pwm":
		return "pwm"
	default:
		return "board"
	}
}

func validateChannelFolders(folders []ChannelFolder) error {
	if len(folders) > MaxPeripheralNames {
		return fmt.Errorf("at most %d folders are allowed", MaxPeripheralNames)
	}
	seen := make(map[string]bool)
	for _, folder := range folders {
		if folder.Kind != "motion" && folder.Kind != "relay" && folder.Kind != "pwm" && folder.Kind != "board" {
			return fmt.Errorf("unknown folder kind %q", folder.Kind)
		}
		name := strings.TrimSpace(folder.Name)
		if name == "" || utf8.RuneCountInString(name) > 64 || !printableText(name) {
			return fmt.Errorf("folder name must be 1..64 printable characters")
		}
		if utf8.RuneCountInString(folder.Icon) > 64 || folder.Icon != "" && !printableText(folder.Icon) {
			return fmt.Errorf("folder icon must be at most 64 printable characters")
		}
		key := folder.Kind + "\x00" + strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("folder %q already exists in %s", name, folder.Kind)
		}
		seen[key] = true
	}
	return nil
}

// Include existing named assignments as well as intentionally empty folders.
func ChannelFolderCatalog(profile BoardProfile, names map[string]string) []ChannelFolder {
	folders := append([]ChannelFolder{}, profile.Folders...)
	_, controls := ProfileDescriptors(profile.Mode, profile.ExposeRawRelays, names, profile.Presentation)
	for _, channel := range controls {
		name := strings.TrimSpace(channel.Group)
		if name == "" {
			continue
		}
		kind := ChannelFolderKind(channel.Kind)
		found := false
		for _, folder := range folders {
			if folder.Kind == kind && strings.EqualFold(folder.Name, name) {
				found = true
				break
			}
		}
		if !found {
			folders = append(folders, ChannelFolder{Kind: kind, Name: name})
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		if folders[i].Kind != folders[j].Kind {
			return folders[i].Kind < folders[j].Kind
		}
		return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
	})
	return folders
}

// ApplyChannelFolderMutation validates a complete transaction before replacing
// the profile. Hidden members participate; deleting a folder only ungroups them.
func ApplyChannelFolderMutation(profile *BoardProfile, names map[string]string, change ChannelFolderMutation) ([]string, error) {
	candidate := *profile
	candidate.Presentation = make(map[string]PeripheralPresentation, len(profile.Presentation))
	for key, value := range profile.Presentation {
		candidate.Presentation[key] = value
	}
	candidate.Folders = ChannelFolderCatalog(candidate, names)
	change.Name = strings.TrimSpace(change.Name)
	if change.Kind == "relay" && (change.Operation == "create" && strings.EqualFold(change.Name, "Raw relays") ||
		change.Operation == "update" && change.NextName != nil && strings.EqualFold(strings.TrimSpace(*change.NextName), "Raw relays")) {
		return nil, fmt.Errorf("Raw relays is reserved for the board's seat wiring")
	}
	index := -1
	for i, folder := range candidate.Folders {
		if folder.Kind == change.Kind && strings.EqualFold(folder.Name, change.Name) {
			index = i
			break
		}
	}
	_, controls := ProfileDescriptors(candidate.Mode, candidate.ExposeRawRelays, names, candidate.Presentation)
	changed := []string{}
	assign := func(control ControlDescriptor, group string) {
		value, exists := candidate.Presentation[control.Key]
		if !exists {
			value.Hidden = control.Hidden
		}
		value.Group = group
		candidate.Presentation[control.Key] = value
		changed = append(changed, control.Key)
	}
	switch change.Operation {
	case "create":
		if index >= 0 {
			return nil, fmt.Errorf("folder %q already exists", change.Name)
		}
		folder := ChannelFolder{Kind: change.Kind, Name: change.Name}
		if change.Icon != nil {
			folder.Icon = strings.TrimSpace(*change.Icon)
		}
		candidate.Folders = append(candidate.Folders, folder)
	case "update", "delete":
		if index < 0 {
			return nil, fmt.Errorf("folder %q does not exist", change.Name)
		}
		old := candidate.Folders[index].Name
		next := old
		if change.Operation == "delete" {
			next = ""
			candidate.Folders = append(candidate.Folders[:index], candidate.Folders[index+1:]...)
		} else {
			if change.NextName != nil {
				next = strings.TrimSpace(*change.NextName)
			}
			candidate.Folders[index].Name = next
			if change.Icon != nil {
				candidate.Folders[index].Icon = strings.TrimSpace(*change.Icon)
			}
		}
		for _, channel := range controls {
			if ChannelFolderKind(channel.Kind) == change.Kind && strings.EqualFold(strings.TrimSpace(channel.Group), old) {
				assign(channel, next)
			}
		}
	case "move":
		if change.Kind != "motion" && change.Kind != "relay" && change.Kind != "pwm" && change.Kind != "board" {
			return nil, fmt.Errorf("unknown folder kind %q", change.Kind)
		}
		if change.Name != "" && index < 0 {
			return nil, fmt.Errorf("destination folder %q does not exist", change.Name)
		}
		if len(change.Keys) == 0 {
			return nil, fmt.Errorf("at least one channel key is required")
		}
		seen := make(map[string]bool)
		for _, key := range change.Keys {
			if seen[key] {
				return nil, fmt.Errorf("duplicate channel key %q", key)
			}
			seen[key] = true
			var found *ControlDescriptor
			for i := range controls {
				if controls[i].Key == key {
					found = &controls[i]
					break
				}
			}
			if found == nil || ChannelFolderKind(found.Kind) != change.Kind {
				return nil, fmt.Errorf("channel %q is not in the %s section", key, change.Kind)
			}
			if found.Control == "seat-internal" {
				return nil, fmt.Errorf("raw seat relay %q cannot be moved into an operator folder", key)
			}
			group := ""
			if index >= 0 {
				group = candidate.Folders[index].Name
			}
			assign(*found, group)
		}
	default:
		return nil, fmt.Errorf("unknown folder operation %q", change.Operation)
	}
	if err := validateChannelFolders(candidate.Folders); err != nil {
		return nil, err
	}
	*profile = candidate
	return changed, nil
}
