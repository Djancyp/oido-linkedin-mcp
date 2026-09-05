package main

import "testing"

func TestNormalizePersonIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare username", "williamhgates", "williamhgates", false},
		{"full profile url", "https://www.linkedin.com/in/williamhgates/", "williamhgates", false},
		{"localized host", "https://de.linkedin.com/in/williamhgates", "williamhgates", false},
		{"mobile touch host", "https://touch.linkedin.com/in/williamhgates", "williamhgates", false},
		{"mwlite wrapper", "https://www.linkedin.com/mwlite/in/williamhgates", "williamhgates", false},
		{"tracking query", "https://www.linkedin.com/in/williamhgates/?trk=abc", "williamhgates", false},
		{"site relative", "/in/williamhgates/", "williamhgates", false},
		{"sub-page suffix", "https://www.linkedin.com/in/williamhgates/recent-activity/all/", "williamhgates", false},
		{"dot segment traversal", "in/alice/../../in/bob", "", true},
		{"double encoded dot segment", "%252e%252e", "", true},
		{"stray percent", "100%off", "", true},
		{"reserved me", "me", "", true},
		{"company link not person", "https://www.linkedin.com/company/microsoft/", "", true},
		{"shortener", "https://lnkd.in/abc123", "", true},
		{"empty", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizePersonIdentifier(c.in, false)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNormalizePersonIdentifierSelfAlias(t *testing.T) {
	got, err := normalizePersonIdentifier("me", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "me" {
		t.Fatalf("got %q, want %q", got, "me")
	}
}

func TestNormalizeCompanyIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare slug", "microsoft", "microsoft", false},
		{"full url", "https://www.linkedin.com/company/microsoft/", "microsoft", false},
		{"person link not company", "https://www.linkedin.com/in/williamhgates/", "", true},
		{"dot segment", "company/microsoft/../../in/bob", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizeCompanyIdentifier(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNormalizeJobID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare numeric", "4252026496", "4252026496", false},
		{"full url", "https://www.linkedin.com/jobs/view/4252026496/", "4252026496", false},
		{"non numeric", "abc", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizeJobID(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseSections(t *testing.T) {
	got := parseSections("experience, contact_info,bogus,skills", personSections)
	want := []string{"experience", "contact_info", "skills"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
