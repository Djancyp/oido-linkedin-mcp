package main

import "strings"

// section maps a section name to the URL suffix appended to a profile or
// company root, ported from linkedin-mcp-server's scraping/fields.py.
type section struct {
	name   string
	suffix string
}

var personSections = []section{
	{"experience", "/details/experience/"},
	{"education", "/details/education/"},
	{"interests", "/details/interests/"},
	{"honors", "/details/honors/"},
	{"languages", "/details/languages/"},
	{"certifications", "/details/certifications/"},
	{"skills", "/details/skills/"},
	{"projects", "/details/projects/"},
	{"contact_info", "/overlay/contact-info/"},
	{"posts", "/recent-activity/all/"},
}

var companySections = []section{
	{"about", "/about/"},
	{"posts", "/posts/"},
	{"jobs", "/jobs/"},
}

func sectionSuffix(sections []section, name string) (string, bool) {
	for _, s := range sections {
		if s.name == name {
			return s.suffix, true
		}
	}
	return "", false
}

// parseSections turns a comma-separated list into the requested section
// names, silently dropping unknown ones (they're just not scraped).
func parseSections(csv string, known []section) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	var out []string
	for _, name := range strings.Split(csv, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := sectionSuffix(known, name); ok {
			out = append(out, name)
		}
	}
	return out
}
