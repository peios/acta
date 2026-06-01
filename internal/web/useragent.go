package web

import "strings"

// userAgentLabel renders a raw User-Agent into a short, friendly device label
// for the session list. It's a deliberately small heuristic — enough to tell
// "Firefox on Linux" from "Safari on iPhone", not a full UA parser.
func userAgentLabel(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return "Unknown device"
	}
	// Order matters: Chrome/Edge/Opera UAs also contain "Safari", and Edge/Opera
	// contain "Chrome", so check the most specific tokens first.
	browser := firstUAMatch(ua, [][2]string{
		{"Edg", "Edge"},
		{"OPR", "Opera"},
		{"Firefox", "Firefox"},
		{"Chrome", "Chrome"},
		{"Safari", "Safari"},
		{"curl", "curl"},
	})
	os := firstUAMatch(ua, [][2]string{
		{"Windows", "Windows"},
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"Android", "Android"},
		{"Macintosh", "macOS"},
		{"Mac OS X", "macOS"},
		{"Linux", "Linux"},
	})
	switch {
	case browser != "" && os != "":
		return browser + " on " + os
	case browser != "":
		return browser
	case os != "":
		return os
	default:
		if len(ua) > 40 {
			return ua[:40] + "…"
		}
		return ua
	}
}

// firstUAMatch returns the label of the first {needle, label} pair whose needle
// is a substring of s, or "" if none match.
func firstUAMatch(s string, pairs [][2]string) string {
	for _, p := range pairs {
		if strings.Contains(s, p[0]) {
			return p[1]
		}
	}
	return ""
}
