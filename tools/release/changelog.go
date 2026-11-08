package main

import (
	"fmt"
	"strings"
)

// Section is a version's section of CHANGELOG.md: the lines under its
// "## <version>" heading, up to the next "## " heading.
type Section struct {
	Version Version
	Line    int    // the heading's line number, from 1
	Notes   string // the lines, trimmed: the release notes
}

// The two parts of every release's notes, as "### " headings.
var languages = []string{"English", "简体中文"}

// ParseChangelog reads a changelog's sections, newest first. Its sections
// are found the way mygo build finds the one it puts in the update
// manifests: a line "## 1.2.0", or "## [1.2.0]" with anything after it, as
// in Keep a Changelog. Lines before the first section, such as the
// "# Changelog" title, are not part of any. It fails when a "## " heading
// is not a MAJOR.MINOR.PATCH version, when the versions are not in
// descending order, and when there is no section at all.
func ParseChangelog(name, text string) ([]Section, error) {
	var sections []Section
	var lines []string
	end := func() {
		if len(sections) > 0 {
			sections[len(sections)-1].Notes = strings.TrimSpace(strings.Join(lines, "\n"))
		}
		lines = nil
	}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		rest, ok := strings.CutPrefix(line, "## ")
		if !ok {
			lines = append(lines, line)
			continue
		}
		end()
		var heading string
		if fields := strings.Fields(rest); len(fields) > 0 {
			heading = fields[0]
			if r, ok := strings.CutPrefix(heading, "["); ok {
				heading, _, _ = strings.Cut(r, "]")
			}
		}
		v, err := ParseVersion(heading)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %q is not a version: a section's heading is \"## MAJOR.MINOR.PATCH\"", name, i+1, line)
		}
		if n := len(sections); n > 0 && v.Compare(sections[n-1].Version) >= 0 {
			return nil, fmt.Errorf("%s:%d: %s comes after %s: sections go newest first, one per version", name, i+1, v, sections[n-1].Version)
		}
		sections = append(sections, Section{Version: v, Line: i + 1})
	}
	end()
	if len(sections) == 0 {
		return nil, fmt.Errorf("%s has no version section: add one, such as \"## 1.2.0\"", name)
	}
	return sections, nil
}

// Find is the section of version v, if there is one.
func Find(sections []Section, v Version) (Section, bool) {
	for _, s := range sections {
		if s.Version == v {
			return s, true
		}
	}
	return Section{}, false
}

// Check fails unless the section has its notes in both languages: a
// "### English" and a "### 简体中文" part, each once and not empty.
func Check(name string, s Section) error {
	parts := map[string]int{} // heading: lines of text under it
	seen := map[string]bool{}
	var part string
	for _, line := range strings.Split(s.Notes, "\n") {
		if h, ok := strings.CutPrefix(line, "### "); ok {
			part = strings.TrimSpace(h)
			if seen[part] {
				return fmt.Errorf("%s: %s has two \"### %s\" parts", name, s.Version, part)
			}
			seen[part] = true
			continue
		}
		if strings.TrimSpace(line) != "" {
			parts[part]++
		}
	}
	for _, lang := range languages {
		switch {
		case !seen[lang]:
			return fmt.Errorf("%s: %s has no \"### %s\" part: every release's notes are in English and Simplified Chinese", name, s.Version, lang)
		case parts[lang] == 0:
			return fmt.Errorf("%s: the \"### %s\" part of %s is empty", name, lang, s.Version)
		}
	}
	return nil
}

// Release is the version to release, or "" for none: the changelog's
// newest section when it is newer than last, the latest release tag (""
// when there is none yet). It fails when that section is older than last,
// or does not pass Check. warning, when not "", says that the version is
// lower than the commits since last call for (see BumpOf); the owner
// chooses the version, so it is only a warning.
func Release(name string, sections []Section, last string, commits []Commit) (version, warning string, err error) {
	newest := sections[0]
	if last != "" {
		lv, err := ParseVersion(last)
		if err != nil {
			return "", "", fmt.Errorf("tag %s: %v", last, err)
		}
		switch c := newest.Version.Compare(lv); {
		case c == 0:
			return "", "", nil
		case c < 0:
			return "", "", fmt.Errorf("%s:%d: the newest section, %s, is older than the last release, %s", name, newest.Line, newest.Version, last)
		}
		if b := BumpOf(commits); b != None {
			if want := lv.Next(b); newest.Version.Compare(want) < 0 {
				warning = fmt.Sprintf("%s is lower than the commits since %s call for: %s (%s)", newest.Version, last, want, b)
			}
		}
	}
	if err := Check(name, newest); err != nil {
		return "", "", err
	}
	return newest.Version.String(), warning, nil
}
