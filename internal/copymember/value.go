// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package copymember

import (
	"errors"
	"fmt"
	"strings"

	cobol "github.com/Zaba505/cobol-go"
)

// ParseValue reads one `--copy-member` value: a member, `=`, and the path of
// the file holding it (docs/cli/SPEC.md, "Where the members come from").
//
// The member is written as the `COPY` statement writes it: a text-name — a
// COBOL word, or an alphanumeric literal in its delimiters — optionally
// followed by `OF` or `IN` and a library-name written the same way. The `=`
// ending it is the first one outside a literal; a COBOL word holds no `=`, and
// a literal ends at its closing delimiter, so the split is unambiguous for
// every member a `COPY` can name, and everything after it is the path,
// whatever it holds.
//
// A literal's delimiters are there for the split and nothing else: the member
// is the literal's content, a doubled delimiter in it undoubled, exactly as
// `cobol-go` hands a resolver the name. So `'STDHDR'` and `STDHDR` are one
// member.
//
// What a word is, and what a literal is, are `cobol-go`'s: the member is read
// with [cobol.Tokenize]. The error says why the value is refused, in words a
// usage error can quote beside the value as typed; nothing is opened.
func ParseValue(value string) (Member, string, error) {
	at := splitAt(value)
	if at < 0 {
		return Member{}, "", errors.New("it has no = outside a literal to end the member at")
	}

	text, path := value[:at], value[at+1:]

	switch {
	case strings.TrimSpace(text) == "":
		return Member{}, "", errors.New("its member, before the =, is empty")
	case path == "":
		return Member{}, "", errors.New("its path, after the =, is empty")
	case path == "-":
		return Member{}, "", errors.New("its path is -, and a fault inside a member is reported at the member's path, which a stream has none of")
	}

	member, err := parseMember(text)
	if err != nil {
		return Member{}, "", err
	}

	return member, path, nil
}

// splitAt is the index of the first `=` outside a literal, or -1.
func splitAt(value string) int {
	var delimiter byte

	for i := 0; i < len(value); i++ {
		c := value[i]

		switch {
		case delimiter != 0 && c == delimiter:
			// A doubled delimiter is one character of the literal, and the
			// literal goes on.
			if i+1 < len(value) && value[i+1] == delimiter {
				i++

				continue
			}

			delimiter = 0
		case delimiter != 0:
		case c == '\'' || c == '"':
			delimiter = c
		case c == '=':
			return i
		}
	}

	return -1
}

// parseMember reads a member: a name, or a name, OF or IN, and a name.
func parseMember(text string) (Member, error) {
	var tokens []cobol.Token

	for token, err := range cobol.Tokenize(strings.NewReader(text)) {
		if err != nil {
			return Member{}, fmt.Errorf("its member is not one COBOL can name: %v", err)
		}

		tokens = append(tokens, token)
	}

	malformed := fmt.Errorf("its member %s is not a COBOL word or literal, optionally followed by OF or IN and another", strings.TrimSpace(text))

	switch len(tokens) {
	case 1:
		name, ok := memberName(tokens[0])
		if !ok {
			return Member{}, malformed
		}

		return Member{Name: name}, nil
	case 3:
		qualifier := strings.ToUpper(string(tokens[1].Value))
		if tokens[1].Type != cobol.TokenIdentifier || (qualifier != "OF" && qualifier != "IN") {
			return Member{}, malformed
		}

		name, nameOK := memberName(tokens[0])
		library, libraryOK := memberName(tokens[2])

		if !nameOK || !libraryOK {
			return Member{}, malformed
		}

		return Member{Name: name, Library: library}, nil
	default:
		return Member{}, malformed
	}
}

// memberName is the name one token of a member carries: a COBOL word as
// written, or an unprefixed alphanumeric literal's content.
func memberName(token cobol.Token) (string, bool) {
	value := string(token.Value)

	switch token.Type {
	case cobol.TokenIdentifier:
		upper := strings.ToUpper(value)

		return value, upper != "OF" && upper != "IN"
	case cobol.TokenString:
		if len(value) < 2 || (value[0] != '\'' && value[0] != '"') {
			// A prefixed literal names no copybook, as `cobol-go` refuses one
			// in a COPY statement.
			return "", false
		}

		delimiter := value[:1]
		content := strings.ReplaceAll(value[1:len(value)-1], delimiter+delimiter, delimiter)

		return content, content != ""
	default:
		return "", false
	}
}
