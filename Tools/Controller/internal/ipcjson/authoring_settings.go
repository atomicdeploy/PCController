package ipcjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"pccontroller.local/controller/internal/appconfig"
)

// These writes do not explicitly execute rules, learn RF, or start playback.
type authoringCollection[T any] struct {
	Items []T    `json:"items"`
	ETag  string `json:"etag"`
}

func authoringDocument[T any](items []T) authoringCollection[T] {
	if items == nil {
		items = []T{}
	}
	return authoringCollection[T]{Items: items, ETag: canonicalSettingsDigest(items)}
}
func updateAuthoringCollection[T any](s *Service, raw json.RawMessage, get func(appconfig.Config) []T, set func(*appconfig.Config, []T), validate func(appconfig.Config) error, event string) (any, error) {
	if len(raw) > 2<<20 {
		return nil, errors.New("authoring request exceeds 2 MiB")
	}
	var p struct {
		Items   *[]T   `json:"items"`
		IfMatch string `json:"if_match"`
	}
	if err := decodeStrictParams(raw, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	if p.Items == nil || len(p.IfMatch) != 64 {
		return nil, errors.New("items and current if_match are required")
	}
	if s.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	prepare := func(c appconfig.Config) (appconfig.Config, error) {
		if authoringDocument(get(c)).ETag != p.IfMatch {
			return c, errors.New("authoring collection changed; refresh before saving")
		}
		set(&c, *p.Items)
		if err := c.Validate(); err != nil {
			return c, err
		}
		if validate != nil {
			if err := validate(c); err != nil {
				return c, err
			}
		}
		return c, nil
	}
	current := s.persistentHostConfig()
	candidate, err := prepare(current)
	if err != nil {
		return nil, err
	}
	if authoringDocument(get(candidate)).ETag == p.IfMatch {
		return authoringDocument(get(current)), nil
	}
	err = s.UpdateHostConfig(func(c *appconfig.Config) error {
		next, err := prepare(*c)
		if err != nil {
			return err
		}
		set(c, get(next))
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Client.EmitHostEvent(event, "Authoring configuration saved")
	return authoringDocument(get(s.persistentHostConfig())), nil
}
func (s *Service) updateAutomations(raw json.RawMessage) (any, error) {
	previous := map[string]string{}
	for _, rule := range s.persistentHostConfig().Automations {
		previous[rule.Name] = canonicalSettingsDigest(rule)
	}
	controls := s.peripheralSettings().Controls
	return updateAuthoringCollection(s, raw, func(c appconfig.Config) []appconfig.Automation { return c.Automations }, func(c *appconfig.Config, v []appconfig.Automation) { c.Automations = v }, func(c appconfig.Config) error {
		seen := map[string]string{}
		for _, r := range c.Automations {
			if !isRFBinding(r) {
				continue
			}
			changed := previous[r.Name] != canonicalSettingsDigest(r)
			if changed && (r.Match.RFCode == nil || *r.Match.RFCode == 0 || r.Match.RFBits == 0 || r.Match.RFBits > 32 || r.Match.RFProtocol == 0 || r.Match.RFProtocol > 12 || r.Match.Gesture == "") {
				return errors.New("RF binding requires complete waveform identity and gesture")
			}
			if changed {
				if err := validateRFControlActions(r.Actions, controls); err != nil {
					return err
				}
			}
			if r.Match.RFCode == nil {
				continue
			}
			if r.Enabled {
				key := fmt.Sprintf("%d/%d/%d/%s", *r.Match.RFCode, r.Match.RFBits, r.Match.RFProtocol, r.Match.Gesture)
				if name := seen[key]; name != "" {
					return fmt.Errorf("button gesture already assigned to %q", name)
				}
				seen[key] = r.Name
			}
		}
		return nil
	}, "automation.configuration")
}
func (s *Service) updateMelodies(raw json.RawMessage) (any, error) {
	return updateAuthoringCollection(s, raw, func(c appconfig.Config) []appconfig.Melody { return c.Melodies }, func(c *appconfig.Config, v []appconfig.Melody) { c.Melodies = v }, nil, "melodies.changed")
}
