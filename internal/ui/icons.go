package ui

import "github.com/egoist/mygo/ui"

// Icons are drawn for DESIGN.md §4, not taken from an icon font.
//
// Interface symbols share one 24×24 board, a 2px round stroke, and
// currentColor. A mark that is only a dot is filled. System marks are
// solid silhouettes in the same board, so a sidebar tile and a block
// header show one drawing at two sizes.

const (
	svgOpen = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`
	svgFill = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">`
)

func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(svgOpen + body + `</svg>`))
}

func iconFill(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(svgFill + body + `</svg>`))
}

// Interface symbols. Strokes sit on even coordinates where the metaphor
// allows it, inside a 20×20 optical box (circles may reach 3–21).
var (
	iconPlus     = icon(`<path d="M12 5v14M5 12h14"/>`)
	iconSearch   = icon(`<circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/>`)
	iconClose    = icon(`<path d="M6 6l12 12M18 6 6 18"/>`)
	iconChevronR = icon(`<path d="m9 5 7 7-7 7"/>`)
	iconChevronD = icon(`<path d="m5 9 7 7 7-7"/>`)
	iconChevronU = icon(`<path d="m5 15 7-7 7 7"/>`)
	iconMore     = icon(`<circle cx="6" cy="12" r="1.35" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.35" fill="currentColor" stroke="none"/><circle cx="18" cy="12" r="1.35" fill="currentColor" stroke="none"/>`)
	iconGrip     = icon(`<circle cx="9" cy="6" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="6" r="1.2" fill="currentColor" stroke="none"/><circle cx="9" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="9" cy="18" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="18" r="1.2" fill="currentColor" stroke="none"/>`)
	iconSidebar  = icon(`<rect x="3.5" y="4" width="17" height="16" rx="3"/><path d="M9 4.5v15"/>`)
	iconMinimap  = icon(`<rect x="3.5" y="4" width="17" height="16" rx="3"/><path d="M8 7.5v9M16 7.5v9M6.5 10h11M6.5 14h11"/>`)
	iconTerminal = icon(`<rect x="3.5" y="4" width="17" height="16" rx="3"/><path d="m8 9 3.5 3L8 15M13 15.5h4"/>`)
	iconSettings = icon(`<path d="M4 8h5M15.5 8H20"/><circle cx="12" cy="8" r="2.25"/><path d="M4 16h2.5M12 16h8"/><circle cx="8.5" cy="16" r="2.25"/>`)
	iconSync     = icon(`<path d="M8 10.5a4.2 4.2 0 0 1 7.2-2.2"/><path d="M14.2 5.6 16.6 8.8 13.2 9.6"/><path d="M16 13.5a4.2 4.2 0 0 1-7.2 2.2"/><path d="M9.8 18.4 7.4 15.2 10.8 14.4"/>`)
	iconTransfer = icon(`<path d="M8 19V6M5 9l3-4 3 4M16 5v13M13 15l3 4 3-4"/>`)
	iconUp       = icon(`<path d="M12 19V5M6 11l6-6 6 6"/>`)
	iconDown     = icon(`<path d="M12 5v14M6 13l6 6 6-6"/>`)
	iconSwap     = icon(`<path d="M5 8h12M14 5l4 3-4 3M19 16H7M10 13l-4 3 4 3"/>`)
	iconRefresh  = icon(`<path d="M19 12.5A7 7 0 1 1 16.2 6"/><path d="M19 4.5V9h-4.5"/>`)
	iconPause    = icon(`<path d="M8 5v14M16 5v14"/>`)
	iconPlay     = icon(`<path d="M8 5.5 17.5 12 8 18.5z"/>`)
	iconEye      = icon(`<path d="M3 12c2.4-3.8 5-5.5 9-5.5s6.6 1.7 9 5.5c-2.4 3.8-5 5.5-9 5.5s-6.6-1.7-9-5.5z"/><circle cx="12" cy="12" r="2.4"/>`)
	iconLock     = icon(`<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8.2a4 4 0 0 1 8 0V11"/>`)
	iconLink     = icon(`<path d="M9 15.5 7.2 17.3a3.3 3.3 0 0 1-4.7-4.7L4.3 10.8"/><path d="m15 8.5 1.8-1.8a3.3 3.3 0 0 1 4.7 4.7L19.7 13.2"/><path d="m9 15 6-6"/>`)
	iconFolder   = icon(`<path d="M4 8.5h5.2L11 6.5h7.2a2 2 0 0 1 2 2V18a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2z"/>`)
	iconFile     = icon(`<path d="M7 3.5h6.2L18 8.2V20.5H7z"/><path d="M13.2 3.5v4.7H18"/>`)
	iconChip     = icon(`<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M10 7V4M14 7V4M10 20v-3M14 20v-3M7 10H4M7 14H4M20 10h-3M20 14h-3"/>`)
	iconCheck    = icon(`<circle cx="12" cy="12" r="8"/><path d="m8 12.2 2.8 2.8 5.4-5.8"/>`)
	iconAlert    = icon(`<path d="M12 4.2 20.2 19.5H3.8z"/><path d="M12 10v4.2"/><circle cx="12" cy="16.8" r="0.9" fill="currentColor" stroke="none"/>`)
)

// System marks. One filled drawing each: Linux is a penguin silhouette
// with no face, Windows is four panes, macOS is an apple with a leaf and
// a bite, and an unknown system is two chassis with no status lights.
// The local terminal uses iconTerminal, the same 2px prompt as the UI.
var (
	// Classic Tux silhouette: head, belly, flippers, feet. No eyes, beak,
	// or other face marks — one filled path keeps the weight even at 18px.
	iconLinux   = iconFill(`<path d="M12 3.2c-2.6 0-4.6 1.7-4.6 4.1 0 .6.1 1.1.3 1.6-2.1.9-3.5 2.6-3.5 4.8 0 1.4.6 2.6 1.5 3.5-.2.5-.3 1-.3 1.5 0 1.6 1.1 2.8 2.8 3.3-.1.3-.1.6-.1.9 0 1.5 1.4 2.5 3.1 2.5h1.6c1.7 0 3.1-1 3.1-2.5 0-.3 0-.6-.1-.9 1.7-.5 2.8-1.7 2.8-3.3 0-.5-.1-1-.3-1.5.9-.9 1.5-2.1 1.5-3.5 0-2.2-1.4-3.9-3.5-4.8.2-.5.3-1 .3-1.6 0-2.4-2-4.1-4.6-4.1z"/>`)
	iconWindows = iconFill(`<rect x="3" y="3" width="8" height="8" rx="1.4"/>` +
		`<rect x="13" y="3" width="8" height="8" rx="1.4"/>` +
		`<rect x="3" y="13" width="8" height="8" rx="1.4"/>` +
		`<rect x="13" y="13" width="8" height="8" rx="1.4"/>`)
	// Apple silhouette: leaf + bitten fruit. No traffic-light chrome.
	iconApple = iconFill(`<path d="M15.1 5.2c.7-.9 1.2-2.1 1.1-3.3-1.1 0-2.4.7-3.2 1.6-.7.8-1.3 2-1.1 3.2 1.3.1 2.5-.6 3.2-1.5z"/>` +
		`<path d="M18.6 8.7c-1.1-.1-2.1.4-2.7.4-.7 0-1.7-.5-3.1-.4-1.6.1-3 1-3.8 2.5-1.6 2.8-.4 7 1.2 9.3.8 1.1 1.7 2.4 2.9 2.3 1.2 0 1.6-.8 3-.8s1.8.8 3 .8c1.3 0 2.1-1.2 2.9-2.3.9-1.3 1.2-2.6 1.2-2.6s-2.4-.9-2.4-3.7c0-2.3 1.8-3.4 1.9-3.5-.9-1.4-2.4-1.8-3.1-2z"/>`)
	iconServer = iconFill(`<rect x="4" y="3.5" width="16" height="6.5" rx="2"/>` +
		`<rect x="4" y="14" width="16" height="6.5" rx="2"/>`)
)
