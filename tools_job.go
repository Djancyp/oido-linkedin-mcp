package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- get_job_details ----

type JobDetailsArgs struct {
	JobID string `json:"job_id" jsonschema:"LinkedIn numeric job id, or /jobs/view/ URL, e.g. \"4252026496\""`
}

func (h *handler) GetJobDetails(ctx context.Context, _ *mcp.CallToolRequest, a JobDetailsArgs) (*mcp.CallToolResult, any, error) {
	id, err := normalizeJobID(a.JobID)
	if err != nil {
		return errResult(err), nil, nil
	}
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, jobViewURL(id)); err != nil {
			return nil, err
		}
		text, err := bodyText(pctx)
		if err != nil {
			return nil, err
		}
		return map[string]string{"job_id": id, "url": jobViewURL(id), "text": text}, nil
	})
}

// ---- search_jobs ----

var (
	dateRangeParam = map[string]string{
		"past_hour":     "r3600",
		"past_24_hours": "r86400",
		"past_week":     "r604800",
		"past_month":    "r2592000",
	}
	jobTypeCode = map[string]string{
		"full_time": "F", "part_time": "P", "contract": "C",
		"temporary": "T", "volunteer": "V", "internship": "I", "other": "O",
	}
	experienceCode = map[string]string{
		"internship": "1", "entry": "2", "associate": "3",
		"mid_senior": "4", "director": "5", "executive": "6",
	}
	workTypeCode = map[string]string{"on_site": "1", "remote": "2", "hybrid": "3"}
)

func csvCodes(csv string, codes map[string]string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(csv, ",") {
		if code, ok := codes[strings.ToLower(strings.TrimSpace(part))]; ok {
			out = append(out, code)
		}
	}
	return out
}

type SearchJobsArgs struct {
	Keywords        string `json:"keywords" jsonschema:"Search keywords, e.g. \"software engineer\", \"data scientist\""`
	Location        string `json:"location,omitempty" jsonschema:"Optional location filter, e.g. \"San Francisco\", \"Remote\""`
	MaxPages        int    `json:"max_pages,omitempty" jsonschema:"Maximum number of result pages to load (1-10, default 3)"`
	DatePosted      string `json:"date_posted,omitempty" jsonschema:"past_hour, past_24_hours, past_week or past_month"`
	JobType         string `json:"job_type,omitempty" jsonschema:"Comma-separated: full_time, part_time, contract, temporary, volunteer, internship, other"`
	ExperienceLevel string `json:"experience_level,omitempty" jsonschema:"Comma-separated: internship, entry, associate, mid_senior, director, executive"`
	WorkType        string `json:"work_type,omitempty" jsonschema:"Comma-separated: on_site, remote, hybrid"`
	EasyApply       bool   `json:"easy_apply,omitempty" jsonschema:"Only show Easy Apply jobs"`
	SortBy          string `json:"sort_by,omitempty" jsonschema:"date or relevance"`
}

type jobSearchResult struct {
	JobID  string `json:"job_id"`
	JobURL string `json:"job_url"`
	Text   string `json:"text"`
}

func (h *handler) SearchJobs(ctx context.Context, _ *mcp.CallToolRequest, a SearchJobsArgs) (*mcp.CallToolResult, any, error) {
	q := url.Values{"keywords": {a.Keywords}}
	if a.Location != "" {
		q.Set("location", a.Location)
	}
	if code, ok := dateRangeParam[a.DatePosted]; ok {
		q.Set("f_TPR", code)
	}
	if codes := csvCodes(a.JobType, jobTypeCode); len(codes) > 0 {
		q.Set("f_JT", strings.Join(codes, ","))
	}
	if codes := csvCodes(a.ExperienceLevel, experienceCode); len(codes) > 0 {
		q.Set("f_E", strings.Join(codes, ","))
	}
	if codes := csvCodes(a.WorkType, workTypeCode); len(codes) > 0 {
		q.Set("f_WT", strings.Join(codes, ","))
	}
	if a.EasyApply {
		q.Set("f_AL", "true")
	}
	if a.SortBy == "date" {
		q.Set("sortBy", "DD")
	} else if a.SortBy == "relevance" {
		q.Set("sortBy", "R")
	}
	maxPages := clampInt(a.MaxPages, 1, 10, 3)
	baseURL := "https://www.linkedin.com/jobs/search/?" + q.Encode()

	return withPage(ctx, func(pctx context.Context) (any, error) {
		var results []jobSearchResult
		seen := map[string]bool{}
		for page := 0; page < maxPages; page++ {
			pageURL := baseURL
			if page > 0 {
				pq := url.Values{}
				for k, v := range q {
					pq[k] = v
				}
				pq.Set("start", strconv.Itoa(page*25))
				pageURL = "https://www.linkedin.com/jobs/search/?" + pq.Encode()
			}
			if err := navigate(pctx, pageURL); err != nil {
				return nil, err
			}
			if err := scrollToBottom(pctx, 3); err != nil {
				return nil, err
			}
			cards, err := extractCards(pctx, `a[href*="/jobs/view/"]`, 30)
			if err != nil {
				return nil, err
			}
			added := 0
			for _, c := range cards {
				id := jobIDFromHref(c.Href)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				results = append(results, jobSearchResult{JobID: id, JobURL: jobViewURL(id), Text: c.Text})
				added++
			}
			if added == 0 {
				break
			}
		}
		return results, nil
	})
}

// ---- get_saved_jobs ----

type SavedJobsArgs struct {
	MaxPages int `json:"max_pages,omitempty" jsonschema:"Maximum number of saved-jobs pages to load (1-10, default 3)"`
}

func (h *handler) GetSavedJobs(ctx context.Context, _ *mcp.CallToolRequest, a SavedJobsArgs) (*mcp.CallToolResult, any, error) {
	maxPages := clampInt(a.MaxPages, 1, 10, 3)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, "https://www.linkedin.com/my-items/saved-jobs/"); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, maxPages*3); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `a[href*="/jobs/view/"]`, 100)
		if err != nil {
			return nil, err
		}
		var results []jobSearchResult
		seen := map[string]bool{}
		for _, c := range cards {
			id := jobIDFromHref(c.Href)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			results = append(results, jobSearchResult{JobID: id, JobURL: jobViewURL(id), Text: c.Text})
		}
		return results, nil
	})
}
