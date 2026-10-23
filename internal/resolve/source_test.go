// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// A source names its kind the way a message does, and a span into it names the
// file its positions are in — which for a definitions file is the file as the
// run opened it rather than the path as the layout spells it.
func TestASourceNamesItsKindAndItsFile(t *testing.T) {
	t.Parallel()

	field := &copybook.Field{Pos: cobol.Pos{Line: 3, Column: 9}}

	for _, test := range []struct {
		source Source
		says   string
		span   diag.Span
	}{
		{
			source: CopybookAt("cpy/orders.cpy"),
			says:   `the copybook "cpy/orders.cpy"`,
			span:   diag.Span{File: "cpy/orders.cpy", Line: 3, Column: 9},
		},
		{
			source: Source{Kind: layoutmodel.DefinitionsFileSource, Spelling: "posting.cpy.sexpr", File: "/srv/ledger/posting.cpy.sexpr"},
			says:   `the definitions file "posting.cpy.sexpr"`,
			span:   diag.Span{File: "/srv/ledger/posting.cpy.sexpr", Line: 3, Column: 9},
		},
		{
			source: Source{Kind: layoutmodel.DefinitionsBlockSource, Spelling: "TRAILER", File: "/srv/ledger/ledger.sexpr"},
			says:   "the definitions block TRAILER",
			span:   diag.Span{File: "/srv/ledger/ledger.sexpr", Line: 3, Column: 9},
		},
		{
			source: Source{Spelling: "x.cpy", File: "x.cpy"},
			says:   `the copybook "x.cpy"`,
			span:   diag.Span{File: "x.cpy", Line: 3, Column: 9},
		},
	} {
		if got := test.source.String(); got != test.says {
			t.Errorf("%+v reads %q, want %q", test.source, got, test.says)
		}

		if got := test.source.Span(field); got != test.span {
			t.Errorf("%+v points at %s, want %s", test.source, got, test.span)
		}

		if got := test.source.Span(nil); got != (diag.Span{File: test.span.File}) {
			t.Errorf("%+v points at %s for no field, want the file alone", test.source, got)
		}
	}
}

// The notes and messages that used to say "copybook" say which kind of source
// the record is held in.
func TestADiagnosticNamesTheKindOfSource(t *testing.T) {
	t.Parallel()

	in := diag.Span{File: "/srv/ledger/posting.cpy.sexpr"}

	for name, test := range map[string]struct {
		err  diag.Error
		want string
	}{
		"a sequencing reference": {
			err:  &SequenceItemError{Copybook: in, Kind: layoutmodel.DefinitionsFileSource, Record: "DEBIT"},
			want: "the record DEBIT is bound to this definitions file",
		},
		"a discriminator's target": {
			err:  &PredicateTargetError{Copybook: in, Kind: layoutmodel.DefinitionsFileSource, Record: "DEBIT"},
			want: "the record DEBIT is bound to this definitions file",
		},
		"an alternative nothing declares": {
			err:  &UnknownAlternativeError{Kind: layoutmodel.DefinitionsFileSource, Names: []string{"A", "B"}},
			want: "the definitions file declares A and B",
		},
		"an alternative a copybook does not declare": {
			err:  &UnknownAlternativeError{Names: []string{"A", "B"}},
			want: "the copybook declares A and B",
		},
	} {
		if got := test.err.Error(); !strings.Contains(got, test.want) {
			t.Errorf("%s reads\n%s\nwant it to say %q", name, got, test.want)
		}
	}
}
