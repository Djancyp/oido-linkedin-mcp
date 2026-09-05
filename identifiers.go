package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Identifier normalization, ported from linkedin-mcp-server's
// scraping/identifiers.py. Every tool argument that names a person, company,
// job or conversation is turned into a bare id here before it becomes a URL
// segment, so a pasted profile link, a mobile/localized host, or a
// path-traversal attempt ("..") all resolve the same safe way (or are
// rejected outright).

var (
	linkedinHostRE = regexp.MustCompile(`^(?:[a-z0-9-]+\.)*linkedin\.com$`)
	shortenerHost  = regexp.MustCompile(`^(?:[a-z0-9-]+\.)*lnkd\.in$`)
	hasSchemeRE    = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	unusableRE     = regexp.MustCompile(`[\s/\\?#]|[\x00-\x1f\x7f]`)
	identifierRE   = regexp.MustCompile(`^[\w-]+$`)
	rawRefusedRE   = regexp.MustCompile(`[\x00-\x1f\x7f\\]`)
	strayPercentRE = regexp.MustCompile(`%([^0-9A-Fa-f]|.?$)`)
	numericIDRE    = regexp.MustCompile(`^[0-9]+$`)
)

var defaultPorts = map[string]string{"http": "80", "https": "443"}
var dotSegments = map[string]bool{".": true, "..": true}
var reservedIdentifiers = map[string]bool{"me": true}

// decodeOnce removes at most one layer of percent-encoding. A result that
// still contains "%" means a second encoding layer was present and is
// refused, never repaired.
func decodeOnce(value string) (string, bool) {
	if !strings.Contains(value, "%") {
		return value, true
	}
	if strayPercentRE.MatchString(value) {
		return "", false
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", false
	}
	if strings.Contains(decoded, "%") {
		return "", false
	}
	return decoded, true
}

func isDotSegment(segment string) bool {
	if dotSegments[segment] {
		return true
	}
	if decoded, ok := decodeOnce(segment); ok && dotSegments[decoded] {
		return true
	}
	return false
}

// usable returns the reference in the form URL builders expect, or ("", false).
func usable(value string) (string, bool) {
	decoded, ok := decodeOnce(value)
	if !ok {
		return "", false
	}
	if decoded == "" || dotSegments[decoded] {
		return "", false
	}
	if unusableRE.MatchString(decoded) {
		return "", false
	}
	return decoded, true
}

// identifier is a public identifier or page slug: narrower than usable.
func identifier(value string) (string, bool) {
	u, ok := usable(value)
	if !ok || !identifierRE.MatchString(u) {
		return "", false
	}
	return u, true
}

// linkedinSegments returns the path segments of a LinkedIn address, or
// (nil, false) if the value is not one. want names what the caller is
// looking for, used only in the shortener error message.
func linkedinSegments(value, want string) ([]string, error) {
	cut := value
	if i := strings.IndexAny(value, "?#"); i >= 0 {
		cut = value[:i]
	}
	if rawRefusedRE.MatchString(cut) {
		return nil, nil
	}

	var candidate string
	switch {
	case hasSchemeRE.MatchString(value):
		candidate = value
	case strings.HasPrefix(value, "//"):
		candidate = "https:" + value
	case strings.HasPrefix(value, "/"):
		candidate = "https://www.linkedin.com" + value
	default:
		candidate = "https://" + value
	}

	u, err := url.Parse(candidate)
	if err != nil {
		return nil, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, nil
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if shortenerHost.MatchString(host) {
		return nil, fmt.Errorf(
			"that is a shortened LinkedIn link, and only a redirect resolves it. "+
				"Open it and pass the %s the address it lands on contains", want)
	}
	if !linkedinHostRE.MatchString(host) {
		return nil, nil
	}
	if port := u.Port(); port != "" && port != defaultPorts[u.Scheme] {
		return nil, nil
	}

	rawSegments := strings.Split(u.EscapedPath(), "/")
	for _, seg := range rawSegments[1 : len(rawSegments)-1] {
		if seg == "" {
			return nil, nil
		}
	}
	segments := make([]string, 0, len(rawSegments))
	for _, seg := range rawSegments {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	for _, seg := range segments {
		if isDotSegment(seg) {
			return nil, nil
		}
	}
	if len(segments) > 0 && strings.ToLower(segments[0]) == "mwlite" {
		segments = segments[1:]
		if len(segments) > 0 && strings.ToLower(segments[0]) == "profile" {
			segments = segments[1:]
		}
	}
	return segments, nil
}

func idAfterRoute(value string, route []string, want string) (string, error) {
	segments, err := linkedinSegments(value, want)
	if err != nil || segments == nil || len(segments) <= len(route) {
		return "", err
	}
	for i, r := range route {
		if strings.ToLower(segments[i]) != r {
			return "", nil
		}
	}
	u, ok := usable(segments[len(route)])
	if !ok {
		return "", nil
	}
	return u, nil
}

// normalizePersonIdentifier returns the public /in/ identifier for a person,
// from a bare identifier or any LinkedIn profile URL shape.
func normalizePersonIdentifier(value string, allowSelfAlias bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("missing linkedin_username (the /in/ public identifier of the person)")
	}
	segments, shortErr := linkedinSegments(value, "/in/ public identifier")
	if shortErr != nil {
		return "", shortErr
	}
	var reference string
	if segments != nil {
		route := ""
		if len(segments) > 0 {
			route = strings.ToLower(segments[0])
		}
		if route != "in" || len(segments) < 2 {
			return "", fmt.Errorf("that is a LinkedIn link but not a personal profile. " +
				`Pass the /in/ public identifier of a person, for example "williamhgates"`)
		}
		ref, ok := identifier(segments[1])
		if !ok {
			return "", fmt.Errorf("that is a LinkedIn link but not a personal profile. " +
				`Pass the /in/ public identifier of a person, for example "williamhgates"`)
		}
		reference = ref
	} else {
		ref, ok := identifier(value)
		if !ok {
			return "", fmt.Errorf("that is not a LinkedIn public identifier. Pass the part after " +
				`/in/ in a profile URL, for example "williamhgates"`)
		}
		reference = ref
	}
	if !allowSelfAlias && reservedIdentifiers[strings.ToLower(reference)] {
		return "", fmt.Errorf(
			`%q is LinkedIn's alias for the signed-in member, not a person you can look up. `+
				"Call linkedin_get_my_profile for the authenticated account's own profile", reference)
	}
	return reference, nil
}

// normalizeCompanyIdentifier returns the /company/ slug for an organization,
// from a bare slug or any LinkedIn company URL shape.
func normalizeCompanyIdentifier(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("missing company_name (the /company/ slug of the organization)")
	}
	segments, shortErr := linkedinSegments(value, "/company/ slug")
	if shortErr != nil {
		return "", shortErr
	}
	if segments != nil {
		route := ""
		if len(segments) > 0 {
			route = strings.ToLower(segments[0])
		}
		if route != "company" || len(segments) < 2 {
			return "", fmt.Errorf("that is a LinkedIn link but not a company page. " +
				`Pass the /company/ slug of an organization, for example "microsoft"`)
		}
		ref, ok := identifier(segments[1])
		if !ok {
			return "", fmt.Errorf("that is a LinkedIn link but not a company page. " +
				`Pass the /company/ slug of an organization, for example "microsoft"`)
		}
		return ref, nil
	}
	ref, ok := identifier(value)
	if !ok {
		return "", fmt.Errorf("that is not a LinkedIn company slug. Pass the part after " +
			`/company/ in an organization URL, for example "microsoft"`)
	}
	return ref, nil
}

// normalizeJobID returns the numeric job id, from a bare id or a
// /jobs/view/<id> URL.
func normalizeJobID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("missing job_id")
	}
	if numericIDRE.MatchString(value) {
		return value, nil
	}
	id, err := idAfterRoute(value, []string{"jobs", "view"}, "job id")
	if err != nil {
		return "", err
	}
	if id != "" && numericIDRE.MatchString(id) {
		return id, nil
	}
	return "", fmt.Errorf(`that is not a LinkedIn job id. Pass the number in /jobs/view/<id>, for example "4252026496"`)
}

// normalizeThreadID returns the messaging thread id, from a bare id or a
// /messaging/thread/<id> URL.
func normalizeThreadID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("missing thread_id")
	}
	if id, ok := identifier(value); ok {
		return id, nil
	}
	id, err := idAfterRoute(value, []string{"messaging", "thread"}, "messaging thread id")
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	return "", fmt.Errorf("that is not a LinkedIn messaging thread id")
}

func personProfileURL(id string) string {
	return "https://www.linkedin.com/in/" + url.PathEscape(id) + "/"
}

func personProfileSectionURL(id, suffix string) string {
	return "https://www.linkedin.com/in/" + url.PathEscape(id) + suffix
}

func companyPageURL(id string) string {
	return "https://www.linkedin.com/company/" + url.PathEscape(id) + "/"
}

func companyPageSectionURL(id, suffix string) string {
	return "https://www.linkedin.com/company/" + url.PathEscape(id) + suffix
}

func jobViewURL(id string) string {
	return "https://www.linkedin.com/jobs/view/" + url.PathEscape(id) + "/"
}

func messagingThreadURL(id string) string {
	return "https://www.linkedin.com/messaging/thread/" + url.PathEscape(id) + "/"
}

// jobIDFromHref extracts the numeric id out of a /jobs/view/<id>... href
// found in card extraction, best-effort.
func jobIDFromHref(href string) string {
	re := regexp.MustCompile(`/jobs/view/(\d+)`)
	m := re.FindStringSubmatch(href)
	if m == nil {
		return ""
	}
	return m[1]
}

// personIDFromHref extracts the /in/<id> segment out of a profile href.
func personIDFromHref(href string) string {
	re := regexp.MustCompile(`/in/([^/?#]+)`)
	m := re.FindStringSubmatch(href)
	if m == nil {
		return ""
	}
	id, ok := identifier(m[1])
	if !ok {
		return m[1]
	}
	return id
}

// companyIDFromHref extracts the /company/<id> segment out of a company href.
func companyIDFromHref(href string) string {
	re := regexp.MustCompile(`/company/([^/?#]+)`)
	m := re.FindStringSubmatch(href)
	if m == nil {
		return ""
	}
	id, ok := identifier(m[1])
	if !ok {
		return m[1]
	}
	return id
}
