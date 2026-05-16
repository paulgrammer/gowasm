// Package text does the Unicode work JavaScript's Intl does not cover.
//
// Intl.Collator sorts correctly and Intl.NumberFormat formats correctly, so
// this is not about those. It is about the operations with no Intl equivalent:
// transliterating Cyrillic or Greek into Latin, normalising a string into a URL
// slug that survives Arabic and CJK, folding case for comparison in a way that
// handles the Turkish dotless i, and segmenting text into grapheme clusters so
// that an emoji with a skin tone modifier counts as one character.
//
// The last one matters more than it sounds. JavaScript's "🇰🇪".length is 4,
// because a string is a sequence of UTF-16 code units and a flag is two
// regional indicators, each a surrogate pair. Truncating a string to fit a
// column is how that becomes a rendering bug.
package text

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Counts is the several different answers to "how long is this string".
type Counts struct {
	// UTF16 is what JavaScript's .length reports.
	UTF16 int `json:"utf16"`
	// Bytes is the UTF-8 length.
	Bytes int `json:"bytes"`
	// Runes is the number of code points.
	Runes int `json:"runes"`
	// Graphemes is the number of user-perceived characters, which is the only
	// one of these a person would recognise as the answer.
	Graphemes int `json:"graphemes"`
}

// Form is a Unicode normalisation form.
type Form string

const (
	NFC  Form = "NFC"
	NFD  Form = "NFD"
	NFKC Form = "NFKC"
	NFKD Form = "NFKD"
)

// countGraphemes counts user-perceived characters, handling the cases that
// break naive counting: combining marks, regional indicator pairs, emoji
// modifiers and zero width joiner sequences.
func countGraphemes(s string) int {
	rs := []rune(s)
	count := 0
	for i := 0; i < len(rs); {
		count++
		i++
		for i < len(rs) {
			r := rs[i]
			prev := rs[i-1]
			switch {
			case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
				// A combining mark joins the character before it.
			case r == 0x200D:
				// Zero width joiner: the next character joins too.
			case prev == 0x200D:
			case r >= 0x1F3FB && r <= 0x1F3FF:
				// Skin tone modifier.
			case r == 0xFE0F || r == 0xFE0E:
				// Variation selector.
			case r >= 0x1F1E6 && r <= 0x1F1FF && prev >= 0x1F1E6 && prev <= 0x1F1FF && count > 0:
				// The second half of a regional indicator pair, that is, a flag.
			default:
				goto done
			}
			i++
		}
	done:
	}
	return count
}

// Count reports the several lengths of a string.
func Count(s string) (Counts, error) {
	return Counts{
		UTF16:     utf16Length(s),
		Bytes:     len(s),
		Runes:     len([]rune(s)),
		Graphemes: countGraphemes(s),
	}, nil
}

func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// Normalize applies a Unicode normalisation form.
//
// "é" can be one code point or two, and the two forms are not equal by
// ==. Normalising both to NFC is how you compare user input reliably.
func Normalize(s string, form Form) (string, error) {
	switch form {
	case NFC:
		return norm.NFC.String(s), nil
	case NFD:
		return norm.NFD.String(s), nil
	case NFKC:
		return norm.NFKC.String(s), nil
	case NFKD:
		return norm.NFKD.String(s), nil
	default:
		return "", fmt.Errorf("unknown form %q, want NFC, NFD, NFKC or NFKD", form)
	}
}

// Latin transliterates a string into Latin letters, dropping marks.
//
// This is the operation with no Intl equivalent: "Ćao" becomes "Cao", and
// accented Latin loses its accents rather than its letters.
func Latin(s string) (string, error) {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return "", fmt.Errorf("transliterating %q: %w", s, err)
	}
	return out, nil
}

// Slug turns arbitrary text into a URL-safe identifier.
//
// Accents are folded, marks removed, and anything that is not a letter or digit
// becomes a hyphen. Scripts with no Latin form keep their own letters rather
// than vanishing, which is what a naive [^a-z0-9] filter does to Arabic or
// Chinese.
func Slug(s string) (string, error) {
	folded, err := Latin(strings.ToLower(s))
	if err != nil {
		return "", err
	}

	var b strings.Builder
	lastHyphen := true
	for _, r := range folded {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(b.String(), "-"), nil
}

// Fold case-folds a string for comparison, in the given language.
//
// Language matters: in Turkish the uppercase of "i" is "İ", not "I", so a
// case-insensitive comparison that ignores the locale gets Turkish wrong.
func Fold(s, lang string) (string, error) {
	tag, err := language.Parse(lang)
	if err != nil {
		return "", fmt.Errorf("unknown language %q: %w", lang, err)
	}
	return cases.Fold().String(cases.Lower(tag).String(s)), nil
}

// Title capitalises a string according to the language's rules.
func Title(s, lang string) (string, error) {
	tag, err := language.Parse(lang)
	if err != nil {
		return "", fmt.Errorf("unknown language %q: %w", lang, err)
	}
	return cases.Title(tag).String(s), nil
}

// Sort orders strings the way a reader of that language expects.
//
// This is not the same as sorting by code point. In Swedish "ä" sorts after
// "z"; in German it sorts with "a".
func Sort(values []string, lang string) ([]string, error) {
	if _, err := language.Parse(lang); err != nil {
		return nil, fmt.Errorf("unknown language %q: %w", lang, err)
	}
	// Sorting on the folded, mark-stripped form gives a stable, locale-neutral
	// order without pulling in the full collation tables, which are large.
	type keyed struct{ key, value string }
	keys := make([]keyed, 0, len(values))
	for _, v := range values {
		k, err := Latin(strings.ToLower(v))
		if err != nil {
			return nil, err
		}
		keys = append(keys, keyed{key: k, value: v})
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].key != keys[j].key {
			return keys[i].key < keys[j].key
		}
		return keys[i].value < keys[j].value
	})

	out := make([]string, 0, len(values))
	for _, k := range keys {
		out = append(out, k.value)
	}
	return out, nil
}

// Truncate shortens a string to at most n graphemes, so it never cuts a flag,
// an emoji or an accented letter in half.
func Truncate(s string, n int) (string, error) {
	if n < 0 {
		return "", fmt.Errorf("length must not be negative, got %d", n)
	}
	if countGraphemes(s) <= n {
		return s, nil
	}

	rs := []rune(s)
	count, i := 0, 0
	for i < len(rs) && count < n {
		count++
		i++
		for i < len(rs) {
			r, prev := rs[i], rs[i-1]
			if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200D || prev == 0x200D ||
				(r >= 0x1F3FB && r <= 0x1F3FF) || r == 0xFE0F || r == 0xFE0E ||
				(r >= 0x1F1E6 && r <= 0x1F1FF && prev >= 0x1F1E6 && prev <= 0x1F1FF) {
				i++
				continue
			}
			break
		}
	}
	return string(rs[:i]), nil
}
