package ui

import "github.com/egoist/mygo/ui"

// icon makes a 24x24 line icon in the style of DESIGN.md §2.3: 1.75 px
// round strokes in the text color.
func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// The icons the UI uses, in the Lucide style.
var (
	iconServer   = icon(`<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01"/>`)
	iconPlus     = icon(`<path d="M12 5v14M5 12h14"/>`)
	iconSearch   = icon(`<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`)
	iconClose    = icon(`<path d="M6 6l12 12M18 6 6 18"/>`)
	iconChevronR = icon(`<path d="m9 6 6 6-6 6"/>`)
	iconChevronD = icon(`<path d="m6 9 6 6 6-6"/>`)
	iconChevronU = icon(`<path d="m6 15 6-6 6 6"/>`)
	iconMore     = icon(`<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`)
	iconTerminal = icon(`<path d="m5 8 5 4-5 4M13 17h6"/><rect x="2" y="4" width="20" height="16" rx="3"/>`)
	iconSettings = icon(`<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>`)
	iconLink     = icon(`<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>`)
	iconLock     = icon(`<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>`)
	iconCheck    = icon(`<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>`)
	iconAlert    = icon(`<path d="M12 3 2 20h20z"/><path d="M12 10v4M12 17h.01"/>`)
	iconRefresh  = icon(`<path d="M20 11a8 8 0 1 0-2 6"/><path d="M20 4v7h-7"/>`)
	iconEye      = icon(`<path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>`)
	iconSidebar  = icon(`<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16"/>`)
	iconMinimap  = icon(`<rect x="3" y="4" width="18" height="16" rx="3"/><rect x="12" y="12" width="6" height="5" rx="1"/>`)
	iconTransfer = icon(`<path d="M7 4v13M4 14l3 3 3-3M17 20V7M14 10l3-3 3 3"/>`)
	iconGrip     = icon(`<circle cx="9" cy="6" r="1"/><circle cx="15" cy="6" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="18" r="1"/><circle cx="15" cy="18" r="1"/>`)
	iconApple    = icon(`<path d="M12 7c1-2 3-3 4-3 0 2-1 4-4 4zM12 8c-3-1-7 1-7 6 0 4 2 7 4 7 1 0 2-1 3-1s2 1 3 1c2 0 4-3 4-6-2-1-3-3-3-5z"/>`)
	iconWindows  = icon(`<path d="M3 5l8-1v8H3zM13 4l8-1v9h-8zM3 13h8v8l-8-1zM13 13h8v9l-8-1z"/>`)
	iconChip     = icon(`<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>`)
	iconPause    = icon(`<path d="M8 5v14M16 5v14"/>`)
	iconPlay     = icon(`<path d="m7 4 13 8-13 8z"/>`)
	iconFolder   = icon(`<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`)
	iconFile     = icon(`<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v4h4"/>`)
	iconUp       = icon(`<path d="M12 19V5M6 11l6-6 6 6"/>`)
	iconDown     = icon(`<path d="M12 5v14M6 13l6 6 6-6"/>`)
	iconSwap     = icon(`<path d="M4 8h14M14 4l4 4-4 4M20 16H6M10 12l-4 4 4 4"/>`)
)
