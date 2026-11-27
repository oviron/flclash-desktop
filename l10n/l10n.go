// Package l10n holds the strings every desktop app shows, keyed by their
// English text, in the files the macOS app reads as they are. The Windows
// parts read the same files, so the two say the same things.
package l10n

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

//go:embed ru.lproj/Localizable.strings
var ruStrings []byte

//go:embed ru.lproj/Localizable.stringsdict
var ruPlurals []byte

var (
	ru        = parseStrings(ruStrings)
	ruForms   = parsePlurals(ruPlurals)
	languages = map[string]map[string]string{"ru": ru}
)

// T returns the text for key in lang, the key itself when there is none.
func T(lang, key string) string {
	if s, ok := languages[lang][key]; ok {
		return s
	}
	return key
}

// F formats the text for key with args, as %@ and %d in the files say.
func F(lang, key string, args ...any) string {
	return fmt.Sprintf(Go(T(lang, key)), args...)
}

// N formats a key whose words follow a count, such as "%d servers".
func N(lang, key string, n int) string {
	if lang == "ru" {
		if forms, ok := ruForms[key]; ok {
			if s, ok := forms[russianForm(n)]; ok {
				return fmt.Sprintf(Go(s), n)
			}
		}
	}
	return F(lang, key, n)
}

// Go turns the specifiers of a strings file into those of fmt.
func Go(s string) string { return strings.ReplaceAll(s, "%@", "%v") }

func russianForm(n int) string {
	switch n10, n100 := n%10, n%100; {
	case n10 == 1 && n100 != 11:
		return "one"
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		return "few"
	default:
		return "many"
	}
}

// parseStrings reads lines of the form "key" = "value"; with the escapes
// the files use.
func parseStrings(data []byte) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		key, rest, ok := quoted(line)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		value, _, ok := quoted(strings.TrimSpace(rest[1:]))
		if ok {
			out[key] = value
		}
	}
	return out
}

// quoted reads one quoted string from the start of s.
func quoted(s string) (string, string, bool) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			v, err := strconv.Unquote(s[:i+1])
			return v, s[i+1:], err == nil
		}
	}
	return "", "", false
}

// parsePlurals reads the plural forms of a stringsdict: key → form → text.
func parsePlurals(data []byte) map[string]map[string]string {
	type node struct {
		XMLName xml.Name
		Text    string `xml:",chardata"`
		Nodes   []node `xml:",any"`
	}
	var plist node
	if xml.Unmarshal(data, &plist) != nil || len(plist.Nodes) == 0 {
		return nil
	}
	// pairs walks a dict: a key, then its value.
	pairs := func(dict node, f func(key string, value node)) {
		for i := 0; i+1 < len(dict.Nodes); i += 2 {
			f(dict.Nodes[i].Text, dict.Nodes[i+1])
		}
	}
	out := map[string]map[string]string{}
	pairs(plist.Nodes[0], func(key string, entry node) {
		forms := map[string]string{}
		pairs(entry, func(name string, value node) {
			if value.XMLName.Local != "dict" {
				return
			}
			pairs(value, func(form string, text node) {
				if text.XMLName.Local == "string" && !strings.HasPrefix(form, "NSString") {
					forms[form] = text.Text
				}
			})
		})
		out[key] = forms
	})
	return out
}
