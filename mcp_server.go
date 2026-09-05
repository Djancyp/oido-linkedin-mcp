package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolTimeout bounds every browser-driven tool call. Without it, a stuck
// page (a challenge that never resolves, a selector that never appears)
// would block the shared tab — and, since every tool call is serialized on
// browserMu, every other tool call too — forever, with no way to recover
// short of restarting the process.
const toolTimeout = 45 * time.Second

type handler struct{}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Error: " + err.Error()}}, IsError: true}
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResult(err), nil, nil
	}
	return textResult(string(b)), nil, nil
}

// withPage serializes every tool call on the one shared browser tab
// (see browserMu in browser.go), bounds it to toolTimeout so a stuck page
// can't wedge the server, and turns launch/scrape/timeout errors into a
// normal tool-error result instead of a transport failure or a hang.
func withPage(reqCtx context.Context, fn func(ctx context.Context) (any, error)) (*mcp.CallToolResult, any, error) {
	browserMu.Lock()
	defer browserMu.Unlock()

	bctx, err := ensureBrowser()
	if err != nil {
		return errResult(err), nil, nil
	}

	// bctx carries chromedp's allocator/target values; wrapping it (rather
	// than reqCtx) with a deadline keeps those while adding the bound. A
	// stop func tied to reqCtx also cancels the call if the MCP client
	// disconnects or its own request context is cancelled.
	tctx, cancel := context.WithTimeout(bctx, toolTimeout)
	defer cancel()
	stopOnClientCancel := context.AfterFunc(reqCtx, cancel)
	defer stopOnClientCancel()

	result, err := fn(tctx)
	if err != nil {
		if tctx.Err() != nil && reqCtx.Err() == nil {
			err = fmt.Errorf("timed out after %s waiting on LinkedIn: %w", toolTimeout, err)
		}
		return errResult(err), nil, nil
	}
	return jsonResult(result)
}

func clampInt(v, min, max, def int) int {
	if v <= 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// RunMCPServer registers every LinkedIn tool and serves over stdio.
func RunMCPServer() {
	defer shutdownBrowser()

	h := &handler{}
	server := mcp.NewServer(&mcp.Implementation{Name: "oido-linkedin", Version: "1.0.0"}, nil)

	// Person
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_person_profile",
		Description: "Scrape a person's LinkedIn profile by /in/ public identifier or profile URL, optionally including extra sections (experience, education, skills, contact_info, posts, ...).",
	}, h.GetPersonProfile)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_people",
		Description: "Search LinkedIn people by keywords, with optional location, connection-degree and current-company filters.",
	}, h.SearchPeople)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_connect_with_person",
		Description: "Send a connection invitation to a person, with an optional note. This changes real account state — use deliberately.",
	}, h.ConnectWithPerson)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_sidebar_profiles",
		Description: "Scrape the \"People also viewed\" / similar-profiles sidebar shown on a person's profile page.",
	}, h.GetSidebarProfiles)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_my_profile",
		Description: "Scrape the authenticated account's own LinkedIn profile.",
	}, h.GetMyProfile)

	// Company
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_company_profile",
		Description: "Scrape a company's LinkedIn page by /company/ slug or URL, optionally including posts and jobs sections.",
	}, h.GetCompanyProfile)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_company_posts",
		Description: "Scrape recent posts from a company's LinkedIn page.",
	}, h.GetCompanyPosts)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_companies",
		Description: "Search LinkedIn companies by keywords.",
	}, h.SearchCompanies)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_company_employees",
		Description: "Scrape a company's employees list, optionally filtered by name, title or skill keywords.",
	}, h.GetCompanyEmployees)

	// Job
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_job_details",
		Description: "Scrape a LinkedIn job posting by numeric job id or /jobs/view/ URL.",
	}, h.GetJobDetails)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_jobs",
		Description: "Search LinkedIn job postings by keywords, with optional location, date-posted, job-type, experience-level, work-type, easy-apply and sort filters.",
	}, h.SearchJobs)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_saved_jobs",
		Description: "Scrape the authenticated account's saved jobs list.",
	}, h.GetSavedJobs)

	// Feed
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_feed",
		Description: "Scrape recent posts from the authenticated account's LinkedIn home feed.",
	}, h.GetFeed)

	// Post
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_posts",
		Description: "Search LinkedIn posts by keywords, with an optional recency filter.",
	}, h.SearchPosts)

	// Messaging
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_inbox",
		Description: "Scrape the authenticated account's LinkedIn messaging inbox: recent conversations and their last message preview.",
	}, h.GetInbox)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_conversation",
		Description: "Scrape a specific messaging conversation's full message history, by participant username, thread id, or inbox position.",
	}, h.GetConversation)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_conversations",
		Description: "Search LinkedIn messaging conversations by keywords.",
	}, h.SearchConversations)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_send_message",
		Description: "Send a LinkedIn message to a person. Requires confirm_send=true — this notifies a real person and cannot be undone.",
	}, h.SendMessage)

	log.Println("Oido LinkedIn MCP Server starting on stdio...")
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		// log.Fatalf calls os.Exit directly, which skips deferred calls —
		// shut the browser down explicitly on this path too.
		shutdownBrowser()
		log.Fatalf("server error: %v", err)
	}
}
