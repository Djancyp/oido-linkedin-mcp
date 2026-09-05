package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type personProfileResult struct {
	LinkedInUsername string            `json:"linkedin_username"`
	URL              string            `json:"url"`
	Sections         map[string]string `json:"sections"`
}

// scrapePersonProfile navigates the main profile page and any requested
// extra sections, collecting each as raw innerText — resilient to LinkedIn's
// obfuscated class names, same rationale as the Python original.
func scrapePersonProfile(ctx context.Context, id string, sections []string, maxScrolls int) (*personProfileResult, error) {
	result := &personProfileResult{LinkedInUsername: id, URL: personProfileURL(id), Sections: map[string]string{}}
	if err := navigate(ctx, personProfileURL(id)); err != nil {
		return nil, err
	}
	text, err := bodyText(ctx)
	if err != nil {
		return nil, err
	}
	result.Sections["main_profile"] = text

	for _, name := range sections {
		suffix, ok := sectionSuffix(personSections, name)
		if !ok {
			continue
		}
		if err := navigate(ctx, personProfileSectionURL(id, suffix)); err != nil {
			return nil, err
		}
		switch name {
		case "posts":
			if err := scrollToBottom(ctx, maxScrolls); err != nil {
				return nil, err
			}
		case "contact_info":
			// overlay page, nothing extra to expand
		default:
			if err := clickShowMore(ctx, `button[aria-label*="more" i]`, maxScrolls); err != nil {
				return nil, err
			}
		}
		t, err := bodyText(ctx)
		if err != nil {
			return nil, err
		}
		result.Sections[name] = t
	}
	return result, nil
}

// ---- get_person_profile ----

type PersonProfileArgs struct {
	LinkedInUsername string `json:"linkedin_username" jsonschema:"LinkedIn /in/ public identifier or profile URL, e.g. \"williamhgates\""`
	Sections         string `json:"sections,omitempty" jsonschema:"Comma-separated extra sections: experience, education, interests, honors, languages, certifications, skills, projects, contact_info, posts. Main profile is always included."`
	MaxScrolls       int    `json:"max_scrolls,omitempty" jsonschema:"Max pagination attempts per section (1-50, default 5)"`
}

func (h *handler) GetPersonProfile(ctx context.Context, _ *mcp.CallToolRequest, a PersonProfileArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizePersonIdentifier(a.LinkedInUsername, false)
	if err != nil {
		return errResult(err), nil, nil
	}
	sections := parseSections(a.Sections, personSections)
	maxScrolls := clampInt(a.MaxScrolls, 1, 50, 5)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		return scrapePersonProfile(pctx, id, sections, maxScrolls)
	})
}

// ---- get_my_profile ----

type MyProfileArgs struct {
	Sections   string `json:"sections,omitempty" jsonschema:"Comma-separated extra sections, same as get_person_profile"`
	MaxScrolls int    `json:"max_scrolls,omitempty" jsonschema:"Max pagination attempts per section (1-50, default 5)"`
}

func (h *handler) GetMyProfile(ctx context.Context, _ *mcp.CallToolRequest, a MyProfileArgs) (*mcp.CallToolResult, any, error) {
	sections := parseSections(a.Sections, personSections)
	maxScrolls := clampInt(a.MaxScrolls, 1, 50, 5)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, "https://www.linkedin.com/in/me/"); err != nil {
			return nil, err
		}
		var loc string
		if err := chromedp.Run(pctx, chromedp.Location(&loc)); err != nil {
			return nil, fmt.Errorf("read redirect: %w", err)
		}
		id := personIDFromHref(loc)
		if id == "" {
			return nil, fmt.Errorf("could not resolve own profile id from redirect %s", loc)
		}
		return scrapePersonProfile(pctx, id, sections, maxScrolls)
	})
}

// ---- search_people ----

type SearchPeopleArgs struct {
	Keywords       string   `json:"keywords" jsonschema:"Search keywords, e.g. \"software engineer\", \"recruiter at Google\""`
	Location       string   `json:"location,omitempty" jsonschema:"Optional location filter, e.g. \"New York\", \"Remote\""`
	Network        []string `json:"network,omitempty" jsonschema:"Optional connection-degree filter: F (1st), S (2nd), O (3rd+ / out of network)"`
	CurrentCompany string   `json:"current_company,omitempty" jsonschema:"Optional current-employer name filter"`
}

type personSearchResult struct {
	LinkedInUsername string `json:"linkedin_username"`
	ProfileURL       string `json:"profile_url"`
	Text             string `json:"text"`
}

func (h *handler) SearchPeople(ctx context.Context, _ *mcp.CallToolRequest, a SearchPeopleArgs) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	keywords := a.Keywords
	if a.Location != "" {
		keywords += " " + a.Location
	}
	if a.CurrentCompany != "" {
		keywords += " " + a.CurrentCompany
	}
	q.Set("keywords", keywords)
	if len(a.Network) > 0 {
		if b, err := json.Marshal(a.Network); err == nil {
			q.Set("network", string(b))
		}
	}
	searchURL := "https://www.linkedin.com/search/results/people/?" + q.Encode()
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, searchURL); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, 3); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `li.reusable-search__result-container, div[data-view-name="search-entity-result-universal-template"]`, 50)
		if err != nil {
			return nil, err
		}
		return peopleFromCards(cards), nil
	})
}

func peopleFromCards(cards []card) []personSearchResult {
	out := make([]personSearchResult, 0, len(cards))
	for _, c := range cards {
		id := personIDFromHref(c.Href)
		if id == "" || strings.EqualFold(id, "me") {
			continue
		}
		out = append(out, personSearchResult{
			LinkedInUsername: id,
			ProfileURL:       personProfileURL(id),
			Text:             c.Text,
		})
	}
	return out
}

// ---- get_sidebar_profiles ----

type SidebarProfilesArgs struct {
	LinkedInUsername string `json:"linkedin_username" jsonschema:"LinkedIn /in/ public identifier or profile URL of the page whose sidebar to scrape"`
}

func (h *handler) GetSidebarProfiles(ctx context.Context, _ *mcp.CallToolRequest, a SidebarProfilesArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizePersonIdentifier(a.LinkedInUsername, false)
	if err != nil {
		return errResult(err), nil, nil
	}
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, personProfileURL(id)); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `aside a[href*="/in/"], .pv-browsemap-section a[href*="/in/"]`, 30)
		if err != nil {
			return nil, err
		}
		return peopleFromCards(cards), nil
	})
}

// ---- connect_with_person ----

type ConnectArgs struct {
	LinkedInUsername string `json:"linkedin_username" jsonschema:"LinkedIn /in/ public identifier or profile URL of the person to invite"`
	Note             string `json:"note,omitempty" jsonschema:"Optional note to include with the invitation"`
}

type connectResult struct {
	LinkedInUsername string `json:"linkedin_username"`
	Status           string `json:"status"`
}

// ConnectWithPerson sends a connection invitation. This changes real account
// state on LinkedIn, so it's deliberately the only person tool with no
// read-only path.
func (h *handler) ConnectWithPerson(ctx context.Context, _ *mcp.CallToolRequest, a ConnectArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizePersonIdentifier(a.LinkedInUsername, false)
	if err != nil {
		return errResult(err), nil, nil
	}
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, personProfileURL(id)); err != nil {
			return nil, err
		}
		text, err := bodyText(pctx)
		if err != nil {
			return nil, err
		}
		lower := strings.ToLower(text)

		clicked, err := clickButtonWithText(pctx, "connect")
		if err != nil {
			return nil, err
		}
		if !clicked {
			if _, err := clickButtonWithText(pctx, "more"); err != nil {
				return nil, err
			}
			if err := chromedp.Run(pctx, chromedp.Sleep(400*time.Millisecond)); err != nil {
				return nil, err
			}
			clicked, err = clickButtonWithText(pctx, "connect")
			if err != nil {
				return nil, err
			}
		}
		if !clicked {
			status := "follow_only"
			if strings.Contains(lower, "1st") || strings.Contains(lower, "message") {
				status = "already_connected"
			}
			return connectResult{LinkedInUsername: id, Status: status}, nil
		}
		if err := chromedp.Run(pctx, chromedp.Sleep(600*time.Millisecond)); err != nil {
			return nil, err
		}

		if a.Note != "" {
			if ok, err := clickButtonWithText(pctx, "add a note"); err != nil {
				return nil, err
			} else if ok {
				if err := chromedp.Run(pctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
					return nil, err
				}
				if err := typeIntoActiveEditable(pctx, `textarea[name="message"], textarea`, a.Note); err != nil {
					return nil, err
				}
			}
		}

		sent, err := clickButtonWithText(pctx, "send without a note")
		if err != nil {
			return nil, err
		}
		if !sent {
			sent, err = clickButtonWithText(pctx, "send invitation")
			if err != nil {
				return nil, err
			}
		}
		if !sent {
			sent, err = clickButtonWithText(pctx, "send")
			if err != nil {
				return nil, err
			}
		}
		status := "pending"
		if !sent {
			status = "unknown_send_confirmation_not_found"
		}
		return connectResult{LinkedInUsername: id, Status: status}, nil
	})
}
