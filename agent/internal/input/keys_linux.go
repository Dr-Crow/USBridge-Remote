//go:build linux

package input

type hidKeySpec struct {
	linuxKey uint16
}

// HID usage code → Linux KEY_* code mapping (US layout)
var hidKeyTable = map[uint8]hidKeySpec{
	4:   {30},  // A
	5:   {48},  // B
	6:   {46},  // C
	7:   {32},  // D
	8:   {18},  // E
	9:   {33},  // F
	10:  {34},  // G
	11:  {35},  // H
	12:  {23},  // I
	13:  {36},  // J
	14:  {37},  // K
	15:  {38},  // L
	16:  {50},  // M
	17:  {49},  // N
	18:  {24},  // O
	19:  {25},  // P
	20:  {16},  // Q
	21:  {19},  // R
	22:  {31},  // S
	23:  {20},  // T
	24:  {22},  // U
	25:  {47},  // V
	26:  {17},  // W
	27:  {45},  // X
	28:  {21},  // Y
	29:  {44},  // Z
	30:  {2},   // 1
	31:  {3},   // 2
	32:  {4},   // 3
	33:  {5},   // 4
	34:  {6},   // 5
	35:  {7},   // 6
	36:  {8},   // 7
	37:  {9},   // 8
	38:  {10},  // 9
	39:  {11},  // 0
	40:  {28},  // Enter
	41:  {1},   // Escape
	42:  {14},  // Backspace
	43:  {15},  // Tab
	44:  {57},  // Space
	45:  {12},  // -
	46:  {13},  // =
	47:  {26},  // [
	48:  {27},  // ]
	49:  {43},  // backslash
	51:  {39},  // ;
	52:  {40},  // '
	53:  {41},  // `
	54:  {51},  // ,
	55:  {52},  // .
	56:  {53},  // /
	57:  {58},  // CapsLock
	58:  {59},  // F1
	59:  {60},  // F2
	60:  {61},  // F3
	61:  {62},  // F4
	62:  {63},  // F5
	63:  {64},  // F6
	64:  {65},  // F7
	65:  {66},  // F8
	66:  {67},  // F9
	67:  {68},  // F10
	68:  {87},  // F11
	69:  {88},  // F12
	73:  {110}, // Insert
	74:  {102}, // Home
	75:  {104}, // PageUp
	76:  {111}, // Delete
	77:  {107}, // End
	78:  {109}, // PageDown
	79:  {106}, // Right
	80:  {105}, // Left
	81:  {108}, // Down
	82:  {103}, // Up
	224: {29},  // Left Ctrl
	225: {42},  // Left Shift
	226: {56},  // Left Alt
	227: {125}, // Left Meta
	228: {97},  // Right Ctrl
	229: {54},  // Right Shift
	230: {100}, // Right Alt
	231: {126}, // Right Meta
}

func hidSpec(key uint8) (hidKeySpec, bool) {
	spec, ok := hidKeyTable[key]
	return spec, ok
}

// asciiToHID maps a printable ASCII rune to the HID usage code Text()
// should press, plus whether Left Shift needs to be held for it -- the
// same US-layout HID codes hidKeyTable above already maps to Linux KEY_*
// codes, so Text() can drive them through the exact same Key() primitive
// Key/Combo already use. Returns ok=false for anything outside printable
// ASCII (control characters other than \n, and any non-ASCII/Unicode
// character -- Cyrillic, CJK, accented Latin, etc.): unlike macOS
// (CGEventKeyboardSetUnicodeString) or Windows (VK_PACKET), Linux's
// uinput only emulates HID scancodes, which fundamentally can't express
// an arbitrary Unicode codepoint without remapping the active XKB layout
// first -- a much larger feature than this no-op-stub-to-working-ASCII
// fix covers. The same caller-facing caveat this MCP tool's own
// description already documents for a non-US host layout applies in the
// other direction here: only the US-layout-reachable subset of Unicode
// (namely ASCII) can be typed at all on Linux today.
func asciiToHID(r rune) (code uint8, shift bool, ok bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return uint8(r-'a') + 4, false, true
	case r >= 'A' && r <= 'Z':
		return uint8(r-'A') + 4, true, true
	case r >= '1' && r <= '9':
		return uint8(r-'1') + 30, false, true
	case r == '0':
		return 39, false, true
	}
	switch r {
	case '\n', '\r':
		return 40, false, true // Enter
	case '\t':
		return 43, false, true
	case ' ':
		return 44, false, true
	case '-':
		return 45, false, true
	case '_':
		return 45, true, true
	case '=':
		return 46, false, true
	case '+':
		return 46, true, true
	case '[':
		return 47, false, true
	case '{':
		return 47, true, true
	case ']':
		return 48, false, true
	case '}':
		return 48, true, true
	case '\\':
		return 49, false, true
	case '|':
		return 49, true, true
	case ';':
		return 51, false, true
	case ':':
		return 51, true, true
	case '\'':
		return 52, false, true
	case '"':
		return 52, true, true
	case '`':
		return 53, false, true
	case '~':
		return 53, true, true
	case ',':
		return 54, false, true
	case '<':
		return 54, true, true
	case '.':
		return 55, false, true
	case '>':
		return 55, true, true
	case '/':
		return 56, false, true
	case '?':
		return 56, true, true
	case '!':
		return 30, true, true
	case '@':
		return 31, true, true
	case '#':
		return 32, true, true
	case '$':
		return 33, true, true
	case '%':
		return 34, true, true
	case '^':
		return 35, true, true
	case '&':
		return 36, true, true
	case '*':
		return 37, true, true
	case '(':
		return 38, true, true
	case ')':
		return 39, true, true
	}
	return 0, false, false
}

// cyrillicToHID maps a Cyrillic letter to the HID usage code of the
// PHYSICAL key that produces it under the standard ЙЦУКЕН layout -- valid
// only while the host's active layout has actually been switched to "ru"
// first (see Text() in controller_linux.go), since HID codes are physical
// positions, not characters: the same uint8 that types 'q' under a US
// layout types 'й' once the host is on ЙЦУКЕН. This is what makes typing
// actual Cyrillic text on Linux possible at all through uinput (which,
// unlike macOS/Windows, has no direct Unicode-injection primitive) --
// switch the host layout to match the script being typed, then drive the
// same positional HID codes asciiToHID already uses for Latin text.
func cyrillicToHID(r rune) (code uint8, shift bool, ok bool) {
	lower := r
	isUpper := r >= 'А' && r <= 'Я' || r == 'Ё'
	if isUpper {
		if r == 'Ё' {
			lower = 'ё'
		} else {
			lower = r - 'А' + 'а'
		}
	}
	c, ok := cyrillicLowerHID[lower]
	return c, isUpper, ok
}

// cyrillicLowerHID: lowercase Cyrillic letter -> physical HID code, per
// the standard ЙЦУКЕН row layout (top/home/bottom rows below match a US
// keyboard's QWERTY/ASDF/ZXCV rows key-for-key).
var cyrillicLowerHID = map[rune]uint8{
	'й': 20, 'ц': 26, 'у': 8, 'к': 21, 'е': 23, 'н': 28, 'г': 24, 'ш': 12, 'щ': 18, 'з': 19, 'х': 47, 'ъ': 48,
	'ф': 4, 'ы': 22, 'в': 7, 'а': 9, 'п': 10, 'р': 11, 'о': 13, 'л': 14, 'д': 15, 'ж': 51, 'э': 52,
	'я': 29, 'ч': 27, 'с': 6, 'м': 25, 'и': 5, 'т': 17, 'ь': 16, 'б': 54, 'ю': 55,
	'ё': 53,
}

// scriptLayout reports which host layout a letter needs ("en"/"ru"), or
// "" for a layout-agnostic character (digit, space, punctuation) that
// types the same either way -- see Text()'s run-splitting loop, which
// only switches layout on an actual script change instead of once per
// character.
func scriptLayout(r rune) string {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return "en"
	case r >= 'а' && r <= 'я', r >= 'А' && r <= 'Я', r == 'ё', r == 'Ё':
		return "ru"
	default:
		return ""
	}
}
