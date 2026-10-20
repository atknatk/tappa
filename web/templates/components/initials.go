package components

import (
	"strings"
	"unicode"
)

// Initials is the two letters drawn in a person's square on People: the first letter of
// the first word and of the last word of the name, upper-cased; one letter for a one-word
// name. It works on runes, so a Maltese name keeps its letter whole (Ċ, Ġ, Ħ, Ż), and it
// skips a leading mark that is not a letter. The square is drawing only -- the template
// hides it from a screen reader, which reads the name beside it.
func Initials(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	first := firstLetter(words[0])
	if len(words) == 1 {
		return first
	}
	return first + firstLetter(words[len(words)-1])
}

func firstLetter(word string) string {
	for _, r := range word {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return string(unicode.ToUpper(r))
		}
	}
	return ""
}
