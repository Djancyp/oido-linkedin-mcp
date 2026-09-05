package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// stealthInitScript patches the most obvious tells that a scraping-detection
// script checks for. It's a small, low-effort subset of what a dedicated
// stealth plugin does — see OIDO.md's "Known gaps" for what's still missing
// (this is an arms race, not a solved problem).
const stealthInitScript = `
Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
window.chrome = window.chrome || { runtime: {} };
Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
// Headless Chromium's software (SwiftShader) WebGL renderer string is one of
// the most reliable headless tells there is. Report a generic real-GPU-like
// string instead of leaving the default, which names SwiftShader outright.
(() => {
	const spoof = (proto) => {
		const orig = proto.getParameter;
		proto.getParameter = function (param) {
			if (param === 37445) return 'Intel Inc.';
			if (param === 37446) return 'Intel Iris OpenGL Engine';
			return orig.call(this, param);
		};
	};
	if (window.WebGLRenderingContext) spoof(WebGLRenderingContext.prototype);
	if (window.WebGL2RenderingContext) spoof(WebGL2RenderingContext.prototype);
})();
`

// browserMu serializes every tool call, and also guards the launch state
// below: ensureBrowser and shutdownBrowser assume the caller already holds
// it (withPage in mcp_server.go is the only caller). LinkedIn UI automation
// drives one shared tab step by step (navigate, click, type) and cannot run
// two tools' worth of clicks interleaved on the same page — this replaces
// the Python server's SequentialToolExecutionMiddleware.
var browserMu sync.Mutex

var (
	browserCtx         context.Context
	browserAllocCancel context.CancelFunc
	browserCtxCancel   context.CancelFunc
	browserUserDataDir string
)

func liAtCookie() string {
	return strings.TrimSpace(os.Getenv("LINKEDIN_LI_AT"))
}

func isHeadless() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("LINKEDIN_HEADLESS")))
	return v != "false" && v != "0"
}

func chromePath() string {
	return strings.TrimSpace(os.Getenv("LINKEDIN_CHROME_PATH"))
}

// ensureBrowser lazily launches one persistent Chrome tab for the process
// lifetime, applies basic stealth flags, and injects the li_at session
// cookie. Later calls reuse the same tab. A failed launch is not cached: the
// next call tries again, so a transient Chrome-launch failure doesn't wedge
// every future tool call for the rest of the process's life.
func ensureBrowser() (context.Context, error) {
	if browserCtx != nil {
		return browserCtx, nil
	}

	cookie := liAtCookie()
	if cookie == "" {
		return nil, fmt.Errorf("LINKEDIN_LI_AT is not set — see OIDO.md for how to get your session cookie")
	}

	// Chromium's process-singleton lock is a Unix-domain socket, capped at
	// ~108 bytes. Passing --user-data-dir alone is not enough to control
	// where that socket ends up: Chromium's crash-reporting/process-
	// singleton init reads $TMPDIR directly (independent of --user-data-dir)
	// for its own scratch directory, and when a stray or invalid --user-
	// data-dir situation arises it falls back to a scratch profile there
	// too. oido-core's per-org/per-user sandboxes set a long $TMPDIR
	// (/sandboxes/orgs/<uuid>/users/<uuid>/workspace/../tmp) — long enough
	// on its own to overflow the socket-path limit — so both the profile
	// dir AND $TMPDIR itself must be pinned short. Overriding TMPDIR on
	// this process (rather than only passing it to the child) means the
	// short value reaches the child via the ordinary, unambiguous single
	// copy in its inherited environment.
	if err := os.Setenv("TMPDIR", "/tmp"); err != nil {
		return nil, fmt.Errorf("set TMPDIR: %w", err)
	}
	userDataDir := fmt.Sprintf("/tmp/oido-linkedin-chrome-%d", os.Getpid())
	if err := os.MkdirAll(userDataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create chrome user-data-dir: %w", err)
	}
	browserUserDataDir = userDataDir

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", isHeadless()),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.UserAgent(defaultUserAgent),
		chromedp.WindowSize(1366, 900),
		chromedp.UserDataDir(userDataDir),
		// This process (and oido-core's containers generally) runs as root,
		// and Chromium refuses to start as root without --no-sandbox.
		// --disable-dev-shm-usage avoids crashes from Docker's default small
		// /dev/shm, which Chromium otherwise uses for shared memory.
		chromedp.NoSandbox,
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	if p := chromePath(); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)

	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(stealthInitScript).Do(ctx)
			return err
		}),
		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie("li_at", cookie).
				WithDomain(".linkedin.com").
				WithPath("/").
				WithSecure(true).
				WithHTTPOnly(true).
				Do(ctx)
		}),
	)
	if err != nil {
		ctxCancel()
		allocCancel()
		_ = os.RemoveAll(userDataDir)
		browserUserDataDir = ""
		return nil, fmt.Errorf("launch browser: %w", err)
	}
	browserCtx = ctx
	browserCtxCancel = ctxCancel
	browserAllocCancel = allocCancel
	return browserCtx, nil
}

// shutdownBrowser closes the shared Chrome tab and its allocator, if one was
// ever launched. Called once as RunMCPServer returns.
func shutdownBrowser() {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserCtxCancel != nil {
		browserCtxCancel()
	}
	if browserAllocCancel != nil {
		browserAllocCancel()
	}
	browserCtx = nil
	if browserUserDataDir != "" {
		_ = os.RemoveAll(browserUserDataDir)
		browserUserDataDir = ""
	}
}

// navigate loads url in the shared tab and waits for the network to go
// mostly idle, then checks for an auth wall before returning.
func navigate(ctx context.Context, url string) error {
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Sleep(time.Duration(1200+rand.Intn(900))*time.Millisecond),
	); err != nil {
		return fmt.Errorf("navigate to %s: %w", url, err)
	}
	return waitAuthOK(ctx)
}

// waitAuthOK is the pragmatic stand-in for the Python server's
// detect_auth_barrier: it checks the page LinkedIn actually served for the
// tell-tale signs of an expired/invalid session cookie, so a caller gets a
// clear error instead of scraping a login page as if it were the profile.
func waitAuthOK(ctx context.Context) error {
	var curURL, body string
	if err := chromedp.Run(ctx,
		chromedp.Location(&curURL),
		chromedp.Evaluate(`document.body ? document.body.innerText.slice(0, 2000) : ""`, &body),
	); err != nil {
		return fmt.Errorf("read page state: %w", err)
	}
	lower := strings.ToLower(curURL)
	if strings.Contains(lower, "linkedin.com/checkpoint") || strings.Contains(lower, "linkedin.com/login") ||
		strings.Contains(lower, "linkedin.com/authwall") {
		return fmt.Errorf("LinkedIn is asking to sign in or verify (%s) — LINKEDIN_LI_AT is likely expired; get a fresh li_at cookie value and update the plugin setting", curURL)
	}
	bodyLower := strings.ToLower(body)
	if strings.Contains(bodyLower, "join now") && strings.Contains(bodyLower, "sign in") &&
		!strings.Contains(bodyLower, "feed") {
		return fmt.Errorf("LinkedIn served a signed-out page — LINKEDIN_LI_AT is likely expired; get a fresh li_at cookie value and update the plugin setting")
	}
	return nil
}

// maxFieldChars caps any single scraped text field. LinkedIn pages (a long
// profile, a loaded-out feed) can produce innerText well past what's useful
// in a tool response; capping keeps one call from blowing up the response
// size the way an uncapped scrape otherwise would.
const maxFieldChars = 20000

func capText(s string) string {
	if len(s) <= maxFieldChars {
		return s
	}
	return s[:maxFieldChars] + fmt.Sprintf("\n… [truncated %d more characters]", len(s)-maxFieldChars)
}

// bodyText returns document.body.innerText for the current page, capped to
// maxFieldChars.
func bodyText(ctx context.Context) (string, error) {
	var text string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &text)); err != nil {
		return "", fmt.Errorf("read page text: %w", err)
	}
	return capText(text), nil
}

// scrollToBottom scrolls the page toward the bottom `times` times, pausing
// between scrolls for lazy-loaded content, mirroring the Python server's
// pagination-by-scroll approach on activity/feed/search pages. Each scroll
// moves a random partial distance rather than jumping straight to
// scrollHeight, with a jittered pause — LinkedIn's most heavily monitored
// pages (search/content especially) are more likely to flag a perfectly
// uniform instant-jump scroll pattern as automation than a slightly uneven
// one. This is a mitigation, not a fix: see OIDO.md's "Known gaps".
func scrollToBottom(ctx context.Context, times int) error {
	for i := 0; i < times; i++ {
		fraction := 0.5 + rand.Float64()*0.5 // scroll 50-100% of one viewport
		js := fmt.Sprintf(`window.scrollBy(0, window.innerHeight * %f)`, fraction)
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(js, nil),
			chromedp.Sleep(time.Duration(700+rand.Intn(700))*time.Millisecond),
		); err != nil {
			return fmt.Errorf("scroll: %w", err)
		}
	}
	return nil
}

// clickButtonWithText clicks the first visible <button> (or aria-labelled
// control) whose text contains needle, case-insensitively. LinkedIn's action
// buttons (Connect, Send, Follow, ...) carry no stable class name, so text
// matching is the resilient anchor here, same rationale as innerText scraping.
func clickButtonWithText(ctx context.Context, needle string) (bool, error) {
	js := fmt.Sprintf(`
		(() => {
			const needle = %q.toLowerCase();
			const candidates = Array.from(document.querySelectorAll('button, [role="button"]'));
			for (const el of candidates) {
				const label = (el.innerText || el.getAttribute('aria-label') || '').trim().toLowerCase();
				if (label.includes(needle)) {
					el.click();
					return true;
				}
			}
			return false;
		})()
	`, needle)
	var clicked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &clicked)); err != nil {
		return false, fmt.Errorf("click %q: %w", needle, err)
	}
	return clicked, nil
}

// typeIntoActiveEditable types text into the first focused/visible
// contenteditable or textarea on the page, used for the messaging composer.
func typeIntoActiveEditable(ctx context.Context, selector, text string) error {
	return chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

// clickShowMore clicks every visible "Show more" button matching selector,
// up to maxClicks times, used on detail sections (experience, skills, ...).
func clickShowMore(ctx context.Context, selector string, maxClicks int) error {
	for i := 0; i < maxClicks; i++ {
		var clicked bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
			(() => {
				const btn = document.querySelector(%q);
				if (!btn) return false;
				btn.click();
				return true;
			})()
		`, selector), &clicked)); err != nil {
			return fmt.Errorf("click show more: %w", err)
		}
		if !clicked {
			return nil
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(700*time.Millisecond)); err != nil {
			return err
		}
	}
	return nil
}

// messagePanelSelector matches LinkedIn's messaging surfaces: the full
// /messaging/thread/ page and the small overlay opened from a profile's
// Message button. Passed straight to querySelector, which accepts a
// comma-separated selector list and returns the first match.
const messagePanelSelector = `.msg-s-message-list-container, .msg-overlay-conversation-bubble__content-wrapper, .msg-overlay-conversation-bubble`

// conversationText reads just the message panel's innerText when one is
// found, instead of the whole page — get_conversation opened via a profile's
// Message button would otherwise return the surrounding profile content
// along with (and dwarfing) the actual chat.
func conversationText(ctx context.Context) (string, error) {
	text, err := extractField(ctx, messagePanelSelector)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) != "" {
		return capText(text), nil
	}
	return bodyText(ctx)
}

// scrollConversationToTop scrolls the message panel's own scroll container
// to the top, up to `times` times, to load LinkedIn's lazy-loaded older
// messages before reading the conversation.
func scrollConversationToTop(ctx context.Context, times int) error {
	js := fmt.Sprintf(`
		(() => {
			const el = document.querySelector(%q);
			if (!el) return false;
			el.scrollTop = 0;
			return true;
		})()
	`, messagePanelSelector)
	for i := 0; i < times; i++ {
		var found bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &found)); err != nil {
			return fmt.Errorf("scroll conversation: %w", err)
		}
		if !found {
			return nil
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(700*time.Millisecond)); err != nil {
			return err
		}
	}
	return nil
}
