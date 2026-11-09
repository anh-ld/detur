package api

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"detur.dev/server/internal/store"
)

func cleanProfiles(in []store.RedirectProfile) ([]store.RedirectProfile, error) {
	if len(in) > maxRedirectProfiles {
		return nil, fmt.Errorf("at most %d redirect profiles", maxRedirectProfiles)
	}
	out := make([]store.RedirectProfile, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return nil, errors.New("redirect profile: name is required")
		}
		if utf8.RuneCountInString(name) > maxProfileNameLen {
			return nil, fmt.Errorf("redirect profile %q: name longer than %d characters", name, maxProfileNameLen)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("redirect profile %q: duplicate name", name)
		}
		seen[strings.ToLower(name)] = true
		prof := store.RedirectProfile{Name: name, IOS: strings.TrimSpace(p.IOS), Android: strings.TrimSpace(p.Android), Web: strings.TrimSpace(p.Web)}
		for _, f := range []struct{ field, v string }{
			{"ios", prof.IOS}, {"android", prof.Android}, {"web", prof.Web},
		} {
			if err := validTarget("redirect profile "+f.field, f.v); err != nil {
				return nil, fmt.Errorf("redirect profile %q: %w", name, err)
			}
		}
		out = append(out, prof)
	}
	return out, nil
}

const (
	maxRedirectProfiles = 10
	maxProfileNameLen   = 40
	maxSocialPresets    = 10
	maxSocialTitle      = 100
	maxSocialDesc       = 300
)

func cleanSocialPresets(in []store.SocialPreset) ([]store.SocialPreset, error) {
	if len(in) > maxSocialPresets {
		return nil, fmt.Errorf("at most %d social presets", maxSocialPresets)
	}
	out := make([]store.SocialPreset, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return nil, errors.New("social preset: name is required")
		}
		if utf8.RuneCountInString(name) > maxProfileNameLen {
			return nil, fmt.Errorf("social preset %q: name longer than %d characters", name, maxProfileNameLen)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("social preset %q: duplicate name", name)
		}
		seen[strings.ToLower(name)] = true
		preset := store.SocialPreset{
			Name:        name,
			Title:       strings.TrimSpace(p.Title),
			Description: strings.TrimSpace(p.Description),
			Image:       strings.TrimSpace(p.Image),
		}
		if utf8.RuneCountInString(preset.Title) > maxSocialTitle {
			return nil, fmt.Errorf("social preset %q: title longer than %d characters", name, maxSocialTitle)
		}
		if utf8.RuneCountInString(preset.Description) > maxSocialDesc {
			return nil, fmt.Errorf("social preset %q: description longer than %d characters", name, maxSocialDesc)
		}
		if preset.Image != "" && !httpsURL(preset.Image) {
			return nil, fmt.Errorf("social preset %q: image must be an absolute https URL", name)
		}
		out = append(out, preset)
	}
	return out, nil
}
