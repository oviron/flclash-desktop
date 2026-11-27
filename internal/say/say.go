// Package say makes the messages a person reads. An error made here reads in
// English through Error(), for the command line and the logs, and in the
// language of the app through In and Text.
package say

import (
	"fmt"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// Error is a message with its format kept, so that it can be put into
// another language after the fact.
type Error struct {
	format string
	args   []any
}

// New makes a message with nothing to fill in.
func New(text string) error { return &Error{format: text} }

// Errorf makes a message from a format, as fmt.Errorf does; %w marks an error
// that is part of the message and stays reachable to errors.Is and errors.As.
func Errorf(format string, args ...any) error { return &Error{format: format, args: args} }

func (e *Error) Error() string { return e.in("") }

// Unwrap gives the errors the message is made of.
func (e *Error) Unwrap() []error {
	var inner []error
	for _, arg := range e.args {
		if err, ok := arg.(error); ok {
			inner = append(inner, err)
		}
	}
	return inner
}

func (e *Error) in(lang string) string {
	format := e.format
	if text, ok := catalogs[lang][format]; ok {
		format = text
	}
	args := make([]any, len(e.args))
	for i, arg := range e.args {
		if err, ok := arg.(error); ok {
			args[i] = In(lang, err)
		} else {
			args[i] = arg
		}
	}
	return fmt.Sprintf(strings.ReplaceAll(format, "%w", "%v"), args...)
}

// joined is several messages read as one, each put into the language on its own.
type joined struct{ parts []error }

// Join makes one message of several, or nil when there are none.
func Join(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return &joined{parts: errs}
}

func (j *joined) Error() string   { return j.in("") }
func (j *joined) Unwrap() []error { return j.parts }

func (j *joined) in(lang string) string {
	texts := make([]string, len(j.parts))
	for i, part := range j.parts {
		texts[i] = In(lang, part)
	}
	return strings.Join(texts, "; ")
}

// In reads an error in the language lang ("ru", or anything else for
// English). An error not made here reads as it is.
func In(lang string, err error) string {
	if err == nil {
		return ""
	}
	switch e := err.(type) {
	case *Error:
		return e.in(lang)
	case *joined:
		return e.in(lang)
	}
	return err.Error()
}

var language atomic.Value // string

// SetLanguage chooses the language of Text: the language the app shows.
func SetLanguage(lang string) { language.Store(lang) }

// Language is the language SetLanguage chose, empty for English.
func Language() string {
	lang, _ := language.Load().(string)
	return lang
}

// Text reads an error in the language of the app, as a sentence: messages
// start in lower case so that they can go inside one another.
func Text(err error) string {
	text := In(Language(), err)
	first, size := utf8.DecodeRuneInString(text)
	if size == 0 {
		return text
	}
	return string(unicode.ToUpper(first)) + text[size:]
}

var catalogs = map[string]map[string]string{"ru": ru}
