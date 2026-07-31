package handler

import "strings"

// coarseDevice turns a User-Agent header into the short label
// sessions.device_info was designed for ("iPhone Safari"), or "" when it cannot
// tell.
//
// WHY NOT PASS r.UserAgent() STRAIGHT THROUGH — which is what
// session.IssueParams.DeviceInfo explicitly warns against, and what this function
// exists to prevent someone doing on a busy afternoon:
//
//   - §4.1. A full user agent is a browser IDENTITY: version, build, platform
//     minutiae. Tappa promises no fingerprinting of the device, and a column
//     holding complete UA strings is a device-print store whatever the column
//     comment says. A label drawn from the fixed vocabulary below carries roughly
//     four bits about the phone — enough for a manager to recognise "which phone
//     is this?" in the M6 device list, useless for tracking.
//   - §7 ("dış girdi handler sınırında doğrulanır"). A header is whatever the
//     client sends: unbounded, arbitrary bytes, control characters, RTL overrides.
//     session.deviceLabel already REJECTS those — which would turn a hostile
//     header into a failed activation for a legitimate employee. Mapping to a
//     closed vocabulary means every value this function returns is a literal
//     written below (or the empty string), so no attacker-controlled byte reaches
//     the column. That is an argument from construction, checkable by reading the
//     returns; TestCoarseDevice pins the mapping and asserts the output carries
//     no input bytes. It is NOT a claim that the downstream validator is
//     unreachable in general — a future caller passing something else still meets
//     it, which is the point of it existing.
//
// ORDER MATTERS and is the only subtle part: Chrome's UA contains "Safari", Edge's
// contains both "Chrome" and "Safari", and Samsung Internet contains all three. The
// most specific token is therefore tested first. Getting this wrong mislabels a
// phone; it cannot leak anything.
//
// "" IS A FIRST-CLASS ANSWER. An unrecognised agent yields no label and the column
// stays NULL — better than guessing, and the session works identically either way.
// Nothing in this product branches on the device label.
func coarseDevice(ua string) string {
	if ua == "" || len(ua) > 2048 {
		// The length gate is not about correctness (the vocabulary below already
		// bounds the output) but about not scanning a megabyte of header text.
		return ""
	}
	platform := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		platform = "iPhone"
	case strings.Contains(ua, "iPad"):
		platform = "iPad"
	case strings.Contains(ua, "Android"):
		platform = "Android"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		platform = "Mac"
	case strings.Contains(ua, "Windows"):
		platform = "Windows"
	case strings.Contains(ua, "Linux"):
		platform = "Linux"
	}

	browser := ""
	switch {
	case strings.Contains(ua, "SamsungBrowser"):
		browser = "Samsung Internet"
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgiOS"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "OPiOS"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS"):
		browser = "Firefox"
	case strings.Contains(ua, "CriOS"), strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}

	switch {
	case platform != "" && browser != "":
		return platform + " " + browser
	case platform != "":
		return platform
	default:
		return browser
	}
}
