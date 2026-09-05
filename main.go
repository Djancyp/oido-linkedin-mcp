package main

import "log"

// Oido LinkedIn MCP server. LINKEDIN_LI_AT (and optional LINKEDIN_HEADLESS /
// LINKEDIN_CHROME_PATH) are injected by oido-core from the extension
// settings; this process drives a real Chrome/Chromium over CDP using that
// session cookie. See OIDO.md for setup and known limitations.
func main() {
	log.Println("Starting Oido LinkedIn MCP Server v1.0.0...")
	RunMCPServer()
}
