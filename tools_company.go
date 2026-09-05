package main

import (
	"context"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type companyProfileResult struct {
	CompanyName string            `json:"company_name"`
	URL         string            `json:"url"`
	Sections    map[string]string `json:"sections"`
}

// ---- get_company_profile ----

type CompanyProfileArgs struct {
	CompanyName string `json:"company_name" jsonschema:"LinkedIn company /company/ slug or URL, e.g. \"docker\", \"microsoft\""`
	Sections    string `json:"sections,omitempty" jsonschema:"Comma-separated extra sections: posts, jobs. \"about\" is always included."`
}

func (h *handler) GetCompanyProfile(ctx context.Context, _ *mcp.CallToolRequest, a CompanyProfileArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizeCompanyIdentifier(a.CompanyName)
	if err != nil {
		return errResult(err), nil, nil
	}
	sections := parseSections(a.Sections, companySections)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		result := &companyProfileResult{CompanyName: id, URL: companyPageURL(id), Sections: map[string]string{}}
		aboutSuffix, _ := sectionSuffix(companySections, "about")
		if err := navigate(pctx, companyPageSectionURL(id, aboutSuffix)); err != nil {
			return nil, err
		}
		text, err := bodyText(pctx)
		if err != nil {
			return nil, err
		}
		result.Sections["about"] = text

		for _, name := range sections {
			if name == "about" {
				continue
			}
			suffix, ok := sectionSuffix(companySections, name)
			if !ok {
				continue
			}
			if err := navigate(pctx, companyPageSectionURL(id, suffix)); err != nil {
				return nil, err
			}
			if err := scrollToBottom(pctx, 3); err != nil {
				return nil, err
			}
			t, err := bodyText(pctx)
			if err != nil {
				return nil, err
			}
			result.Sections[name] = t
		}
		return result, nil
	})
}

// ---- get_company_posts ----

type CompanyPostsArgs struct {
	CompanyName string `json:"company_name" jsonschema:"LinkedIn company /company/ slug or URL"`
}

func (h *handler) GetCompanyPosts(ctx context.Context, _ *mcp.CallToolRequest, a CompanyPostsArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizeCompanyIdentifier(a.CompanyName)
	if err != nil {
		return errResult(err), nil, nil
	}
	return withPage(ctx, func(pctx context.Context) (any, error) {
		suffix, _ := sectionSuffix(companySections, "posts")
		if err := navigate(pctx, companyPageSectionURL(id, suffix)); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, 4); err != nil {
			return nil, err
		}
		text, err := bodyText(pctx)
		if err != nil {
			return nil, err
		}
		return map[string]string{"company_name": id, "posts_text": text}, nil
	})
}

// ---- search_companies ----

type SearchCompaniesArgs struct {
	Keywords string `json:"keywords" jsonschema:"Search keywords, e.g. \"fintech\", \"anthropic\", \"electric vehicles\""`
}

type companySearchResult struct {
	CompanyName string `json:"company_name"`
	CompanyURL  string `json:"company_url"`
	Text        string `json:"text"`
}

func (h *handler) SearchCompanies(ctx context.Context, _ *mcp.CallToolRequest, a SearchCompaniesArgs) (*mcp.CallToolResult, any, error) {
	q := url.Values{"keywords": {a.Keywords}}
	searchURL := "https://www.linkedin.com/search/results/companies/?" + q.Encode()
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
		out := make([]companySearchResult, 0, len(cards))
		for _, c := range cards {
			id := companyIDFromHref(c.Href)
			if id == "" {
				continue
			}
			out = append(out, companySearchResult{CompanyName: id, CompanyURL: companyPageURL(id), Text: c.Text})
		}
		return out, nil
	})
}

// ---- get_company_employees ----

type CompanyEmployeesArgs struct {
	CompanyName string `json:"company_name" jsonschema:"LinkedIn company /company/ slug or URL"`
	Keywords    string `json:"keywords,omitempty" jsonschema:"Optional filter by name, job title, or skill"`
}

func (h *handler) GetCompanyEmployees(ctx context.Context, _ *mcp.CallToolRequest, a CompanyEmployeesArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizeCompanyIdentifier(a.CompanyName)
	if err != nil {
		return errResult(err), nil, nil
	}
	q := url.Values{}
	if a.Keywords != "" {
		q.Set("keywords", a.Keywords)
	}
	searchURL := companyPageSectionURL(id, "/people/")
	if len(q) > 0 {
		searchURL += "?" + q.Encode()
	}
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, searchURL); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, 3); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `li[class*="org-people-profile-card"], a[href*="/in/"]`, 60)
		if err != nil {
			return nil, err
		}
		return peopleFromCards(cards), nil
	})
}
