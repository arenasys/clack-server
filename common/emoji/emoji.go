package emoji

import (
	"clack/common"
	_ "embed"
	"fmt"
	"strings"
)

func EmojiToCodepoint(emoji string) string {
	vs := rune(0xFE0F)  // Variation Selector-16
	zwj := rune(0x200D) // Zero Width Joiner

	hasZWJ := false
	for _, r := range []rune(emoji) {
		if r == zwj {
			hasZWJ = true
			break
		}
	}

	var parts []string
	for _, r := range []rune(emoji) {
		if !hasZWJ && r == vs {
			continue
		}
		parts = append(parts, fmt.Sprintf("%x", r))
	}

	return strings.Join(parts, "-")
}

var CodepointToID = map[string]int64{}
var IDToCodepoint = map[int64]string{}

func IsUnicodeEmojiID(id int64) bool {
	_, exists := IDToCodepoint[id]
	return exists
}

//go:embed unicode_emojis.txt
var unicode_emojis_string string

func init() {
	lines := strings.Split(unicode_emojis_string, "\n")
	for _, codepoint := range lines {
		id := int64(common.HashCRC32(codepoint)) // uint32 is too small to collide with real Snowflakes

		if _codepoint, exists := IDToCodepoint[id]; exists && _codepoint != codepoint {
			panic(fmt.Sprintf("ID collision: %d for codepoints %s and %s", id, codepoint, _codepoint))
		}

		CodepointToID[codepoint] = id
		IDToCodepoint[id] = codepoint
	}
}
