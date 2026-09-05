# Oido LinkedIn

Scrape and act on LinkedIn — profiles, companies, jobs, posts, feed, and
messaging — via MCP, by driving a real Chrome session with your own
`li_at` session cookie. There is no official LinkedIn API here; this
automates the same pages a signed-in browser sees.

This is a Go/chromedp port of
[linkedin-mcp-server](https://github.com/) with a **reduced anti-detection
surface** — see "Known gaps" below before relying on it heavily.

## Setup

1. Log into linkedin.com in a normal browser.
2. Open devtools → Application (Chrome) or Storage (Firefox) → Cookies →
   `https://www.linkedin.com` → find the cookie named `li_at` → copy its
   value.
3. Fill the extension settings:
   - **LINKEDIN_LI_AT** — the `li_at` cookie value (sensitive).
   - **LINKEDIN_HEADLESS** — leave `true` unless debugging.
   - **LINKEDIN_CHROME_PATH** — only if Chrome/Chromium isn't auto-found.
4. Save. Verify with `linkedin_get_my_profile`.

The cookie expires periodically (LinkedIn rotates it, and it's tied to
your account's session). When a tool call fails with a "sign in or
verify" error, get a fresh `li_at` value and update the setting.

## Tools

Person

- `linkedin_get_person_profile` — profile by `linkedin_username` (or full
  URL), with optional `sections` (experience, education, interests,
  honors, languages, certifications, skills, projects, contact_info,
  posts) and `max_scrolls`.
- `linkedin_search_people` — by `keywords`, `location`, `network`
  (`F`/`S`/`O`), `current_company`.
- `linkedin_connect_with_person` — sends a connection invite, optional
  `note`. **Changes real account state.**
- `linkedin_get_sidebar_profiles` — "People also viewed" on a profile.
- `linkedin_get_my_profile` — same shape as `get_person_profile`, for the
  authenticated account.

Company

- `linkedin_get_company_profile` — by `company_name`, optional `sections`
  (posts, jobs; about is always included).
- `linkedin_get_company_posts`
- `linkedin_search_companies` — by `keywords`.
- `linkedin_get_company_employees` — optional `keywords` filter.

Job

- `linkedin_get_job_details` — by `job_id`.
- `linkedin_search_jobs` — `keywords`, `location`, `max_pages`,
  `date_posted`, `job_type`, `experience_level`, `work_type`,
  `easy_apply`, `sort_by`.
- `linkedin_get_saved_jobs`

Feed & posts

- `linkedin_get_feed` — `num_posts`.
- `linkedin_search_posts` — `keywords`, `date_posted`, `max_pages`.

Messaging

- `linkedin_get_inbox` — `limit`.
- `linkedin_get_conversation` — by `linkedin_username`, `thread_id`, or
  inbox `index`.
- `linkedin_search_conversations` — `keywords`, `limit` (filters the
  loaded inbox locally; LinkedIn's own messaging search has no reliable
  URL query).
- `linkedin_send_message` — requires `confirm_send: true`. **Notifies a
  real person and cannot be undone.**

## Known gaps vs. the Python original

This port trades depth for a much smaller, dependency-light Go binary.
Specifically, it does **not** include:

- WebRTC/WebGL fingerprint hardening, or any of the Patchright-level
  stealth patches. Basic measures only: disabled automation flags,
  `navigator.webdriver` patched, a spoofed WebGL vendor/renderer string
  (real headless Chromium reports "SwiftShader", a well-known headless
  tell), a realistic desktop User-Agent, and jittered (not perfectly
  uniform) scroll/navigation timing.
- Rate-limit detection/backoff. If LinkedIn starts throttling, tools will
  just start returning odd or empty results — slow down manually.
- **Bot-detection avoidance beyond the above is fundamentally an arms
  race** — there is no configuration that makes a headless, server-side
  browser indistinguishable from a real signed-in user's browser and
  behavior. LinkedIn's `search/results/*` pages (`linkedin_search_posts`,
  `linkedin_search_people`, `linkedin_search_companies`,
  `linkedin_search_jobs`) are its most heavily monitored surface, since
  they're the primary target of commercial scraping/lead-gen tools — a
  checkpoint or a logged-out `li_at` session is more likely to come from
  repeated or rapid use of these than from the read-a-single-profile
  tools. If you hit a checkpoint: stop calling LinkedIn tools for a while,
  get a fresh `li_at` cookie once you can browse linkedin.com normally in
  a real browser again, and space out search calls rather than running
  several back-to-back.
- Proxy support.
- A daemon or multiple rotating profiles. One Chrome profile per
  org/user is reused across processes/restarts (see "Persistent browser
  profile" below) — not the pooled, rotated identities the Python
  original supports.
- Deep auth-wall detection. `waitAuthOK` only checks for the obvious
  checkpoint/login/authwall URLs and a signed-out-looking body — a subtler
  LinkedIn challenge page may still be scraped as if it were real content.

All tool calls are serialized on that one shared tab (no concurrent
scraping), since LinkedIn UI automation is inherently step-by-step. Each
call is bounded to a 45s timeout so a stuck page fails that one call
instead of wedging every later call too; a failed browser launch is
retried on the next call rather than being cached as a permanent error.
Scraped text fields are capped (~20KB for a full page, smaller for list
cards) so one large page can't blow up a tool response.

## Persistent browser profile

A brand-new headless browser signing straight into LinkedIn and
immediately scraping looks like a fresh device automating from the first
second — itself a signal, on top of headless Chromium's own fingerprint
tells. To reduce that, Chrome's profile is **not** a throwaway per-process
temp dir: it lives at `$HOME/.oido-linkedin-chrome-profile`, where `$HOME`
is set by oido-core to the calling org/user's own directory on the
persistent `oido_sandboxes` volume — so history, cookies, cache and
localStorage accumulate across calls and restarts the way a real
returning user's browser would, instead of resetting every time.

- Falls back to the old throwaway `/tmp/oido-linkedin-chrome-<pid>`
  behavior (deleted on exit) when `$HOME` isn't set or writable — e.g.
  running the binary standalone, as the manual smoke test above does.
- A profile directory isn't safe for two Chrome instances at once, so an
  exclusive lock file (`<profile>.lock`) guards it: an overlapping call
  for the same org/user waits (up to the 45s tool timeout) rather than
  colliding with an in-progress one.
- Growth is unbounded in this pass — Chrome's cache/history/etc.
  accumulate on the persistent volume with no automatic pruning. Worth
  watching if disk usage on that volume becomes a concern.

## Notes

- Selectors target stable structural anchors (`a[href*="/in/"]`, button
  text like "Connect"/"Message"/"Send") rather than LinkedIn's obfuscated
  CSS classes, but LinkedIn's markup does change over time — expect to
  need selector touch-ups in `extract_cards.go` / `browser.go` /
  `tools_*.go` occasionally.
- `linkedin_connect_with_person` and `linkedin_send_message` are the two
  tools that change real account state or contact a real person. Use them
  deliberately.
- A Chrome or Chromium binary must be present on the host; chromedp
  auto-discovers common install paths, or set `LINKEDIN_CHROME_PATH`.
- Chrome runs with `--no-sandbox` (oido-core's containers run as root,
  and Chromium refuses to start as root without it) and a fixed short
  `/tmp/oido-linkedin-chrome-<pid>` user-data-dir, bypassing `$TMPDIR` —
  in oido-core's per-org/per-user sandboxes that variable is long enough
  on its own to overflow the ~108-byte limit on the Unix-domain socket
  Chrome's process-singleton lock uses, which otherwise fails Chrome's
  launch with "Socket path too long".
- A `sched_getscheduler: Function not implemented` warning from crashpad
  in the logs is benign — it's the sandboxed seccomp profile blocking a
  syscall crashpad merely probes for, not a launch failure.
