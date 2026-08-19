// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestAnOffsetIsTheSumOfTheWidthsAheadOfIt is the criterion this whole story
// turns on, held against a record where getting it wrong is possible.
//
// docs/ir/SPEC.md's "Ordering and width, and no offset" carries no offset on any
// node deliberately, so that a producer cannot state one wrongly — and names the
// cost: "every consumer is free to get it wrong on its own." This generator is
// one of those consumers, and this is the test that makes its arithmetic a thing
// somebody checked rather than a thing somebody wrote.
//
// The record is chosen so that four separate mistakes are visible: an item
// carrying no logical value that still occupies bytes, a slack run, a group
// whose members are laid out from the group's own offset, and a table whose
// count is data.
func TestAnOffsetIsTheSumOfTheWidthsAheadOfIt(t *testing.T) {
	t.Parallel()

	// Every row, and not only the ones named below: the lookups say the rows
	// this test names are right, and this says they are all the rows there are.
	// See [tabled] for why the key alone cannot do it.
	rowCount(t, variableAutomaton(), "VARIABLE-RECORD", 17)

	rows := tabled(t, variableAutomaton(), "VARIABLE-RECORD")

	for _, want := range []struct {
		item, at, extent string
	}{
		{"REC-TYPE", "0", "1"},
		{"ENTRY-COUNT", "1", "2"},
		{"*slack*", "3", "1"},

		// The table itself, and everything inside one occurrence of it, is laid
		// out from the table's own offset: the sum counts one occurrence — the
		// first — of every group that encloses an item and repeats.
		{"ENTRIES", "4", "4 × ENTRY-COUNT"},
		{"ENTRIES.ENTRY-KIND", "4", "1"},
		{"ENTRIES.*variant*", "5", "3"},

		// Everything behind the table carries the variable term, and the fixed
		// parts of the two go on adding up independently.
		{"TRAILERS", "4 + 4 × ENTRY-COUNT", "12"},
		{"TRAILERS.TRAILER-TAG", "4 + 4 × ENTRY-COUNT", "2"},
		{"TRAILERS.TRAILER-SEQ", "6 + 4 × ENTRY-COUNT", "2"},

		// An INDEX item has no logical value the IR describes and carries a
		// width all the same, "so that the sum stays correct across it".
		{"INDEX-SLOT", "16 + 4 × ENTRY-COUNT", "4"},
	} {
		row, ok := rows[want.item]
		if !ok {
			t.Errorf("the table for VARIABLE-RECORD carries no row for %s", want.item)

			continue
		}

		if row.at != want.at {
			t.Errorf("%s begins at %q, and the widths ahead of it sum to %q", want.item, row.at, want.at)
		}

		if row.extent != want.extent {
			t.Errorf("%s is %q wide, want %q", want.item, row.extent, want.extent)
		}
	}
}

// TestEveryArmOfAVariantBeginsAtTheVariantsFirstByte is docs/ir/SPEC.md's "A
// variant is chosen once per occurrence" as the table has to draw it.
//
// The arms are alternatives over one run of bytes, so they share an offset. A
// table that laid them out one behind another would describe a record where all
// of them are present at once, which is the one thing an alternation does not
// mean — and it would put every offset behind the variant too far along.
func TestEveryArmOfAVariantBeginsAtTheVariantsFirstByte(t *testing.T) {
	t.Parallel()

	rows := tabled(t, variableAutomaton(), "VARIABLE-RECORD")

	for _, name := range []string{"ENTRIES.*variant*", "ENTRIES.CASH", "ENTRIES.CASH.CASH-AMOUNT", "ENTRIES.CHEQUE-NUMBER"} {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("the table for VARIABLE-RECORD carries no row for %s", name)
		}

		if row.at != "5" {
			t.Errorf("%s begins at %q, and every arm of the variant begins at byte 5", name, row.at)
		}
	}

	// And each arm says what chooses it, which is the only thing that tells one
	// alternative from another: they have no names of their own, and the
	// redefined item's name is carried on the first arm alone.
	for name, want := range map[string]string{
		"ENTRIES.CASH":          "when ENTRIES.ENTRY-KIND = 0xC1",
		"ENTRIES.CHEQUE-NUMBER": "when ENTRIES.ENTRY-KIND = 0xC3",
	} {
		if got := rows[name].present; got != want {
			t.Errorf("%s is present %q, want %q", name, got, want)
		}
	}
}

// TestARepetitionSaysHowManyTimes covers the three things the presence column
// can say about how often an item is there.
//
// The bounds are drawn beside a variable count because docs/ir/SPEC.md carries
// them "for that check and for nothing else" — a count outside them is malformed
// data — and they are the copybook's own `OCCURS integer-1 TO integer-2`, which
// is what a reader with the copybook in front of them is comparing against.
func TestARepetitionSaysHowManyTimes(t *testing.T) {
	t.Parallel()

	d := variableAutomaton()

	for _, testCase := range []struct {
		record, item, want string
	}{
		{"VARIABLE-RECORD", "REC-TYPE", always},
		{"VARIABLE-RECORD", "ENTRIES", "occurs ENTRY-COUNT times (1 to 20)"},
		{"VARIABLE-RECORD", "TRAILERS", "occurs 3 times"},
		{"PICTURE-RECORD", "TAGS", "occurs 4 times"},

		// A count held in a register rather than in this record, which is how a
		// header's field sizes a later record's table. It is named the way the
		// register table beside the diagram names it.
		{"PICTURE-RECORD", "EXTRAS", "occurs r20 times (0 to 9)"},
	} {
		if got := tabled(t, d, testCase.record)[testCase.item].present; got != testCase.want {
			t.Errorf("%s.%s is present %q, want %q", testCase.record, testCase.item, got, testCase.want)
		}
	}
}

// TestThePictureIsSpelledFromTheFiveFactsTheIRCarries walks the whole of what a
// picture column can say.
//
// Every category, signed and unsigned, every sign position, and a scale in each
// of the places it can be: none, inside the digits, at their left, and past
// either end where a picture carries `P` positions that occupy no storage. Held
// as a table rather than only in the golden because the golden shows a
// representative row of each and this shows that no combination is drawn as
// something else.
//
// The usage is stated on every case because the SIGN clause is a fact about the
// pair: it has an effect on DISPLAY and on nothing else, so which sign positions
// are admissible depends on which usage is asking. See [signClause].
func TestThePictureIsSpelledFromTheFiveFactsTheIRCarries(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		picture *irpb.Picture
		width   uint32
		usage   irpb.Usage
		want    string
	}{
		"an unsigned integer":       {numeric(4, 0), 4, irpb.Usage_USAGE_DISPLAY, "9(4)"},
		"a scale inside the digits": {numeric(5, 2), 5, irpb.Usage_USAGE_DISPLAY, "9(3)V9(2)"},
		"a scale at the left":       {numeric(3, 3), 3, irpb.Usage_USAGE_DISPLAY, "V9(3)"},

		// A picture opening with a run of P: the value is scaled down past the
		// digits that are stored, and the implied point is at the P's left.
		"a scale past the digits": {numeric(2, 5), 2, irpb.Usage_USAGE_DISPLAY, "P(3)9(2)"},

		// And one ending in a run of P, which is what a negative scale is.
		"a negative scale": {numeric(3, -2), 3, irpb.Usage_USAGE_DISPLAY, "9(3)P(2)"},

		// A signed item of a usage the SIGN clause has no effect on: the `S` is
		// the whole of what there is to say, and stating a position would be a
		// contradiction rather than detail. See the refusals below.
		"a signed packed item": {
			signedNumeric(4, 0, irpb.SignPosition_SIGN_POSITION_UNSPECIFIED), 4,
			irpb.Usage_USAGE_PACKED_DECIMAL, "S9(4)",
		},
		"a signed binary item": {
			signedNumeric(4, 0, irpb.SignPosition_SIGN_POSITION_UNSPECIFIED), 2,
			irpb.Usage_USAGE_BINARY, "S9(4)",
		},

		// And every position a signed DISPLAY item can state, the default
		// included: SIGN TRAILING is a position rather than an absence, and a
		// column that left it out would make a blank mean two things.
		"a leading sign": {
			signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_LEADING), 3,
			irpb.Usage_USAGE_DISPLAY, "S9(3) SIGN LEADING",
		},
		"a trailing sign": {
			signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_TRAILING), 3,
			irpb.Usage_USAGE_DISPLAY, "S9(3) SIGN TRAILING",
		},
		"a separate leading sign": {
			signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_LEADING_SEPARATE), 4,
			irpb.Usage_USAGE_DISPLAY, "S9(3) SIGN LEADING SEPARATE",
		},
		"a separate trailing sign": {
			signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE), 4,
			irpb.Usage_USAGE_DISPLAY, "S9(3) SIGN TRAILING SEPARATE",
		},
		"a signed scaled item": {
			signedNumeric(7, 2, irpb.SignPosition_SIGN_POSITION_TRAILING), 7,
			irpb.Usage_USAGE_DISPLAY, "S9(5)V9(2) SIGN TRAILING",
		},

		// The character count of these two is the item's width and not its
		// digit count: `digits` counts 9 symbols and neither picture has one.
		"alphabetic": {
			&irpb.Picture{Category: irpb.Category_CATEGORY_ALPHABETIC}, 6,
			irpb.Usage_USAGE_DISPLAY, "A(6)",
		},
		"alphanumeric": {
			&irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC}, 6,
			irpb.Usage_USAGE_DISPLAY, "X(6)",
		},

		// An edited item is named and nothing of it is spelled — not even the
		// digit count, which counts 9 symbols and so under-reports every
		// position a Z or a * suppresses. Whether the item is signed makes no
		// difference for the same reason: an edited sign is a CR or a DB in the
		// mask, and the mask is not carried.
		"alphanumeric-edited": {
			&irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC_EDITED}, 6,
			irpb.Usage_USAGE_DISPLAY, alphanumericEdited,
		},
		"numeric-edited": {
			numericEditedPicture(5, 2, false), 9, irpb.Usage_USAGE_DISPLAY, numericEdited,
		},
		"a signed numeric-edited item": {
			numericEditedPicture(5, 2, true), 9, irpb.Usage_USAGE_DISPLAY, numericEdited,
		},
		"a numeric-edited item carrying no digits at all": {
			numericEditedPicture(0, 0, false), 9, irpb.Usage_USAGE_DISPLAY, numericEdited,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := pictureOf(testCase.picture, testCase.width, testCase.usage)
			if err != nil {
				t.Fatalf("pictureOf: %v", err)
			}

			if got != testCase.want {
				t.Errorf("the picture is spelled %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestAPictureThatContradictsItselfIsRefused is the other half of the column
// above, and the reason it is a refusal rather than a rendering.
//
// Each of these is a descriptor stating two facts that cannot both be true, and
// each has two drawings that are equally defensible and equally wrong — print
// the sign clause and describe a sign the item does not hold, or drop it and
// discard something the descriptor states. There is no honest picture to draw,
// which is the same judgment [framingOf] makes of a framing outside the closed
// set.
func TestAPictureThatContradictsItselfIsRefused(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		picture *irpb.Picture
		usage   irpb.Usage
		names   string
	}{
		// The sign axis in all three of the ways it can contradict itself. The
		// first two are mirror images and both are refused, which is the point:
		// refusing one and drawing the other would leave a signed DISPLAY item
		// with no position rendering as a bare `S9(3)`, which is the blank
		// [signClause]'s own argument says must not happen.
		"an unsigned item stating where its sign sits": {
			picture: &irpb.Picture{
				Category:     irpb.Category_CATEGORY_NUMERIC,
				Digits:       3,
				SignPosition: irpb.SignPosition_SIGN_POSITION_TRAILING,
			},
			usage: irpb.Usage_USAGE_DISPLAY,
			names: "unsigned states where its operational sign sits",
		},
		"a signed numeric DISPLAY item stating no position": {
			picture: &irpb.Picture{
				Category: irpb.Category_CATEGORY_NUMERIC,
				Digits:   3,
				Signed:   true,
			},
			usage: irpb.Usage_USAGE_DISPLAY,
			names: "signed numeric DISPLAY item states nothing about where its operational sign sits",
		},
		"a packed item stating a position the SIGN clause cannot reach": {
			picture: &irpb.Picture{
				Category:     irpb.Category_CATEGORY_NUMERIC,
				Digits:       3,
				Signed:       true,
				SignPosition: irpb.SignPosition_SIGN_POSITION_LEADING,
			},
			usage: irpb.Usage_USAGE_PACKED_DECIMAL,
			names: "USAGE PACKED-DECIMAL that is signed states where its operational sign sits",
		},
		"a sign position outside the closed set": {
			picture: &irpb.Picture{
				Category:     irpb.Category_CATEGORY_NUMERIC,
				Digits:       3,
				Signed:       true,
				SignPosition: irpb.SignPosition(99),
			},
			usage: irpb.Usage_USAGE_DISPLAY,
			names: "sign position 99",
		},
		"an alphanumeric item carrying an operational sign": {
			picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC, Signed: true},
			usage:   irpb.Usage_USAGE_DISPLAY,
			names:   "carries an operational sign",
		},
		"a numeric item with no digit positions": {
			picture: &irpb.Picture{Category: irpb.Category_CATEGORY_NUMERIC},
			usage:   irpb.Usage_USAGE_DISPLAY,
			names:   "no stored digit positions",
		},
		"a picture stating no category": {
			picture: &irpb.Picture{Digits: 3},
			usage:   irpb.Usage_USAGE_DISPLAY,
			names:   "states no category",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := pictureOf(testCase.picture, 4, testCase.usage)
			if err == nil {
				t.Fatalf("pictureOf accepted %s", name)
			}

			if !strings.Contains(err.Error(), testCase.names) {
				t.Errorf("the refusal reads %q, and does not name %q", err, testCase.names)
			}
		})
	}
}

// TestAUsageDecidesWhetherAPictureBelongs holds both halves of the rule that a
// picture is present exactly where the item's USAGE has one to resolve.
//
// Both directions are refusals and neither is fussiness. A picture missing where
// one belongs would leave the cell blank, which reads as a generator that could
// not fill it in; a picture present where the IR derives none is a producer
// stating something about an item its own schema says nothing is derived for,
// and drawing it would put that in a document a reader is about to trust.
func TestAUsageDecidesWhetherAPictureBelongs(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		field   *irpb.Field
		usage   string
		picture string
		refused string
	}{
		"a display item": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_DISPLAY, Width: 4, Picture: numeric(4, 0)},
			usage:   "DISPLAY",
			picture: "9(4)",
		},
		"a packed item": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_PACKED_DECIMAL, Width: 3, Picture: numeric(5, 0)},
			usage:   "PACKED-DECIMAL",
			picture: "9(5)",
		},
		"a comp-6 item": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_COMP_6, Width: 2, Picture: numeric(4, 0)},
			usage:   "COMP-6",
			picture: "9(4)",
		},
		"a binary item": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_BINARY, Width: 2, Picture: numeric(4, 0)},
			usage:   "BINARY",
			picture: "9(4)",
		},
		"a comp-5 item": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_COMP_5, Width: 4, Picture: numeric(9, 0)},
			usage:   "COMP-5",
			picture: "9(9)",
		},

		// The five usages the IR derives no picture for, each of which carries a
		// width so that the sum stays correct across it and nothing else.
		"a short float":   {field: &irpb.Field{Usage: irpb.Usage_USAGE_COMP_1, Width: 4}, usage: "COMP-1", picture: notAnItem},
		"a long float":    {field: &irpb.Field{Usage: irpb.Usage_USAGE_COMP_2, Width: 8}, usage: "COMP-2", picture: notAnItem},
		"an index":        {field: &irpb.Field{Usage: irpb.Usage_USAGE_INDEX, Width: 4}, usage: "INDEX", picture: notAnItem},
		"a pointer":       {field: &irpb.Field{Usage: irpb.Usage_USAGE_POINTER, Width: 4}, usage: "POINTER", picture: notAnItem},
		"a national item": {field: &irpb.Field{Usage: irpb.Usage_USAGE_NATIONAL, Width: 8}, usage: "NATIONAL", picture: notAnItem},

		// An item whose layout gave it no charset. The picture is spelled as
		// the copybook wrote it and the column says the bytes are not
		// characters, which is the one thing about the item nothing else in
		// the row states.
		"a display item no charset governs": {
			field: &irpb.Field{
				Usage: irpb.Usage_USAGE_DISPLAY, Width: 8,
				Picture:  &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
				Encoding: &irpb.Encoding{Charset: irpb.Charset_CHARSET_NONE},
			},
			usage:   "DISPLAY",
			picture: "X(8), no charset",
		},

		// The same charset on a usage it does not govern. A packed item's
		// bytes are not characters under any charset, so there is nothing for
		// the column to add and it adds nothing.
		"a packed item whose encoding says none": {
			field: &irpb.Field{
				Usage: irpb.Usage_USAGE_PACKED_DECIMAL, Width: 3, Picture: numeric(5, 0),
				Encoding: &irpb.Encoding{Charset: irpb.Charset_CHARSET_NONE},
			},
			usage:   "PACKED-DECIMAL",
			picture: "9(5)",
		},

		"a display item carrying no picture": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_DISPLAY, Width: 4},
			refused: "USAGE DISPLAY carries no picture",
		},
		"an index carrying one": {
			field:   &irpb.Field{Usage: irpb.Usage_USAGE_INDEX, Width: 4, Picture: numeric(4, 0)},
			refused: "USAGE INDEX carries a picture",
		},
		"an item stating no usage at all": {
			field:   &irpb.Field{Width: 4},
			refused: "USAGE 0",
		},
		"an item stating a usage outside the closed set": {
			field:   &irpb.Field{Usage: irpb.Usage(99), Width: 4},
			refused: "USAGE 99",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			usage, picture, err := described(testCase.field)

			if testCase.refused != "" {
				if err == nil {
					t.Fatalf("described accepted %s", name)
				}

				if !strings.Contains(err.Error(), testCase.refused) {
					t.Errorf("the refusal reads %q, and does not name %q", err, testCase.refused)
				}

				return
			}

			if err != nil {
				t.Fatalf("described: %v", err)
			}

			if usage != testCase.usage || picture != testCase.picture {
				t.Errorf("the item is drawn as %q %q, want %q %q", usage, picture, testCase.usage, testCase.picture)
			}
		})
	}
}

// TestOneRecordIsTabledOnceHoweverManyTransitionsAdmitIt is why the tables hang
// off the records and not off the edges.
//
// The counted automaton admits HEADER-RECORD from three different states,
// because a header may follow a header, a summary or the start of the file.
// Position is stated once, as ordering and width, and nothing about a transition
// changes it — so three tables would be three copies of one fact and a reader
// comparing two of them would be looking for a difference that cannot exist.
func TestOneRecordIsTabledOnceHoweverManyTransitionsAdmitIt(t *testing.T) {
	t.Parallel()

	g := drawn(t, countedAutomaton())

	drawnRecords := make([]string, 0, len(g.records))
	for _, r := range g.records {
		drawnRecords = append(drawnRecords, r.name)
	}

	// In the order the diagram first admits each, which is the order a file
	// holds them in rather than the order the descriptor carries the nodes.
	if want := "HEADER-RECORD,DETAIL-RECORD,SUMMARY-RECORD"; strings.Join(drawnRecords, ",") != want {
		t.Errorf("the records tabled are %v, want %s", drawnRecords, want)
	}
}

// TestRecordsNoneOmitsTheTablesEntirely is the option, from the side that says
// it is a walk that does not happen rather than a section left out at the end.
//
// The descriptor is one whose records are perfectly well formed, so what this
// holds is the option and not a refusal — and the document beside it,
// testdata/counted-records-none.md, is the same file with the section gone.
func TestRecordsNoneOmitsTheTablesEntirely(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, countedAutomaton(), optFlag, recordsOption+"="+recordsNone)

	if strings.Contains(written, mermaidRecords) {
		t.Errorf("records=none wrote the record section:\n%s", written)
	}

	// And the diagram is untouched: the option governs the tables beneath it and
	// nothing above.
	if !strings.Contains(written, "s2 --> s3: HEADER-RECORD") {
		t.Errorf("records=none dropped something above the tables:\n%s", written)
	}
}

// TestAMalformedItemIsOnlyReadWhereItIsDrawn is the other half of the option,
// and the reason [read] takes the options rather than the emitter alone.
//
// A record carrying an item the descriptor does not describe is a refusal under
// `records=all`, because the table has a cell it cannot fill. Under
// `records=none` there is no cell: refusing there would be refusing a diagram
// over something nobody looking at the diagram could see, which is the same
// judgment [fieldPath] makes about a record's top level.
func TestAMalformedItemIsOnlyReadWhereItIsDrawn(t *testing.T) {
	t.Parallel()

	// A field carrying no USAGE at all, which is a producer bug and not a
	// copybook a reader could fix.
	broken := oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		&irpb.Node{
			Id:   101,
			Kind: &irpb.Node_Field{Field: &irpb.Field{Width: 1, Names: &irpb.Names{Original: "TYPE-CODE"}}},
		})

	if _, err := read(broken, defaults()); err == nil {
		t.Error("read drew a table over an item the descriptor does not describe")
	}

	sequencing := options{records: recordsNone}.defaulted()

	if _, err := read(broken, sequencing); err != nil {
		t.Errorf("records=none refused a descriptor over an item it does not draw: %v", err)
	}
}

// TestAContainmentOrderThatContainsItselfIsRefused keeps a producer bug from
// becoming a stack overflow.
//
// A member list states containment downward, so a group holding one of its own
// ancestors is a descriptor no producer may emit. A walk that met one would
// recurse until the stack was gone, and that is a failure no diagnostic of this
// program composes — a reader would be told nothing at all.
func TestAContainmentOrderThatContainsItselfIsRefused(t *testing.T) {
	t.Parallel()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		groupNode(105, "HEADER-RECORD", 103),
		groupNode(103, "ENTRY", 105),
	), defaults())

	if err == nil {
		t.Fatal("read accepted a group that contains itself")
	}

	if !strings.Contains(err.Error(), "contains itself") {
		t.Errorf("the refusal reads %q, and does not say what is wrong with the descriptor", err)
	}
}

// TestAVariantWhoseArmsAreNotOneWidthIsRefused is the requirement that keeps a
// variant's contribution to the sum a constant.
//
// docs/ir/SPEC.md has every arm's extent equal every other arm's, "so that
// nothing else in this schema moves for it". It is checked here because this is
// where getting it wrong is invisible: the table would draw every offset behind
// the variant at the position the first arm implies, and look perfectly well
// formed doing it.
func TestAVariantWhoseArmsAreNotOneWidthIsRefused(t *testing.T) {
	t.Parallel()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		groupNode(105, "HEADER-RECORD", 101, 400),
		variantNode(400, armAt(60, 401), armAt(61, 402)),
		fieldNode(401, "SHORT-ARM", 2),
		fieldNode(402, "LONG-ARM", 5),
		equalPredicate(60, 101, "\xc1"),
		equalPredicate(61, 101, "\xc3"),
	), defaults())

	if err == nil {
		t.Fatal("read accepted a variant whose arms cover different numbers of bytes")
	}

	if !strings.Contains(err.Error(), "is 5 bytes and its first arm is 2") {
		t.Errorf("the refusal reads %q, and does not name the two widths", err)
	}
}

// TestARecordHoldingNoItemSaysSo is the item table's version of
// [graph.admitsNothing].
//
// A record whose top level holds nothing describes no bytes, and a heading over
// a table with no rows reads as a generator that gave up halfway. It gets a
// sentence instead — the same choice this document makes everywhere it would
// otherwise leave a blank where a value goes.
func TestARecordHoldingNoItemSaysSo(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true, 30),
			transitionNode(30, 4, 2),
			recordNode(4, "EMPTY-RECORD", ""),
			groupNode(20, "EMPTY-RECORD"),
		},
	})

	if !strings.Contains(written, "### EMPTY-RECORD") {
		t.Errorf("the document tables no record called EMPTY-RECORD:\n%s", written)
	}

	if !strings.Contains(written, mermaidNoItems) {
		t.Errorf("the document says nothing about a record holding no item:\n%s", written)
	}

	if strings.Contains(written, mermaidRecordHeader) {
		t.Errorf("the document drew a table with no rows in it:\n%s", written)
	}
}

// TestAFillerIsDrawnRatherThanRefused is the case that makes the difference
// between this generator working on real copybooks and not.
//
// COBOL's FILLER has no data-name, so a producer emits no names message for it,
// and FILLER is in most copybooks anybody actually has. Refusing it would refuse
// the whole document — `records=all` is the default — so a layout with one
// unnamed item would have got no diagram either, over an item nobody was looking
// at. It is drawn as a row with this generator's own word, exactly as slack and
// a variant are.
//
// A FILLER group as well as a FILLER field, because a group is the case a slack
// node could not stand in for: it holds members, and they are named beneath it.
func TestAFillerIsDrawnRatherThanRefused(t *testing.T) {
	t.Parallel()

	// The last four rows of the record, in order. By position rather than by
	// Item cell, because the FILLER field and the FILLER group print the same
	// cell — which is what [rowsOf] is for.
	rows := rowsOf(t, variableAutomaton(), "VARIABLE-RECORD")

	want := []row{
		{item: "*filler*", at: "20 + 4 × ENTRY-COUNT", extent: "2"},

		// The group, then the named item inside it. The group contributes its
		// word to the path rather than a name, so its member reads beneath it —
		// a member of a FILLER group that read as a member of the record would
		// put the item at a level of the copybook it is not at.
		{item: "*filler*", at: "22 + 4 × ENTRY-COUNT", extent: "1"},
		{item: "*filler*.NOTE-CODE", at: "22 + 4 × ENTRY-COUNT", extent: "1"},

		// And a table of one-byte occurrences, whose whole width is the count.
		{item: "FLAGS", at: "23 + 4 × ENTRY-COUNT", extent: "ENTRY-COUNT"},
	}

	last := rows[len(rows)-len(want):]

	for at, one := range want {
		if last[at].item != one.item || last[at].at != one.at || last[at].extent != one.extent {
			t.Errorf("row %d from the end is %s, %q wide at %q; want %s, %q at %q",
				len(want)-at, last[at].item, last[at].extent, last[at].at, one.item, one.extent, one.at)
		}
	}
}

// TestAnItemCarryingNoNameIsRefused is the other side of the FILLER rule: a
// names message that states nothing.
//
// An item COBOL names nothing carries no names message at all. One that carries
// a message and states no name in it is a named item whose name went missing,
// which is a producer bug — and whitespace passes an emptiness test and draws as
// a cell holding a space, which reads as an item this generator could not name.
// Same refusal [edgeAt] makes of a record name.
func TestAnItemCarryingNoNameIsRefused(t *testing.T) {
	t.Parallel()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		fieldNode(101, "  ", 1),
	), defaults())

	if err == nil {
		t.Fatal("read accepted an item carrying no name a table could show")
	}

	if !strings.Contains(err.Error(), "carries a names message, and states no name in it") {
		t.Errorf("the refusal reads %q, and does not say what is missing", err)
	}
}

// TestAContainmentOrderThatDoesNotSayWhatItSaysIsRefused walks the refusals the
// walk makes that no other test reaches.
//
// Each is a `malformed` naming a node and a rule, and each is written once and
// never run — which is how a diagnostic ends up naming the wrong identifier, or
// the wrong rule, and nobody finds out until it fires on somebody's descriptor.
// They are cheap to exercise and the message is the whole value of them.
func TestAContainmentOrderThatDoesNotSayWhatItSaysIsRefused(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		nodes []*irpb.Node
		names string
	}{
		"a variant carrying one arm": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 400),
				variantNode(400, armAt(60, 401)),
				fieldNode(401, "ONLY-ARM", 2),
				equalPredicate(60, 401, "\xc1"),
			},
			names: "variant 400 carries 1 arms",
		},

		// The check that keeps a variant's contribution to every offset behind
		// it a constant. Without it the table draws them all at the position
		// the first arm implies and looks perfectly well formed doing it.
		"an arm holding a table whose count is data": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 102, 400),
				variantNode(400, armGroupAt(60, 402), armAt(61, 405)),
				groupNode(402, "COUNTED-ARM", 403),
				repeatingFieldNode(403, "ENTRY", 2, fieldCount(102, 1, 9)),
				fieldNode(405, "FLAT-ARM", 4),
				equalPredicate(60, 102, "\xc1"),
				equalPredicate(61, 102, "\xc3"),
			},
			names: "data-dependent number of times",
		},
		"an arm with no body at all": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 400),
				variantNode(400, &irpb.Arm{PredicateId: 60}, armAt(61, 401)),
				fieldNode(401, "OTHER-ARM", 2),
				equalPredicate(60, 401, "\xc1"),
				equalPredicate(61, 401, "\xc3"),
			},
			names: "an arm of variant 400 has no body",
		},
		"an arm naming a field where its body says group": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 400),
				variantNode(400, armGroupAt(60, 401), armAt(61, 402)),
				fieldNode(401, "NOT-A-GROUP", 2),
				fieldNode(402, "OTHER-ARM", 2),
				equalPredicate(60, 402, "\xc1"),
				equalPredicate(61, 402, "\xc3"),
			},
			// Caught by the walk that resolves the arm's predicate, which
			// descends the variant looking for the predicate's target and
			// refuses the body before the arm itself is read. An earlier
			// refusal for the same fault, and the one a reader is sent to.
			names: "the body of an arm of a variant in a record a transition admits names node 401",
		},

		// A member list may name a group, a variant, a field or a slack node,
		// and nothing else. A state node in one is a producer that lost track of
		// which namespace it was in.
		"a member list naming a node of a kind it may not hold": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 2),
			},
			names: "node 2 is not something a group may contain",
		},
		"an item that repeats and says nothing about how many times": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 401),
				repeatingFieldNode(401, "ENTRY", 2, &irpb.Repetition{}),
			},
			names: "says nothing about how many times",
		},
		"a variable count naming neither a field nor a register": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 401),
				repeatingFieldNode(401, "ENTRY", 2, &irpb.Repetition{
					Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{}},
				}),
			},
			names: "says nothing about where its count is read from",
		},
		"a variable count naming a register the descriptor does not carry": {
			nodes: []*irpb.Node{
				groupNode(105, "HEADER-RECORD", 401),
				repeatingFieldNode(401, "ENTRY", 2, registerCount(900, 0, 9)),
			},
			names: "the register an OCCURS DEPENDING ON count is read from names node 900",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := read(oneRecordAutomaton(
				append([]*irpb.Node{edgeNode(30, 100, 2, nil, nil, nil)}, testCase.nodes...)...,
			), defaults())

			if err == nil {
				t.Fatalf("read accepted %s", name)
			}

			if !strings.Contains(err.Error(), testCase.names) {
				t.Errorf("the refusal reads %q, and does not name %q", err, testCase.names)
			}
		})
	}
}

// row is one row of a record's table, reduced to the strings a document prints.
type row struct {
	item, at, extent, usage, picture, present string
}

// rowsOf is a record's rows in the order the document prints them.
//
// The order is what a test needs when it names a row whose Item cell is not
// unique: two FILLER items at one level both read `*filler*`, as do two slack
// runs and two variants, because each is this generator's word for a node the
// copybook did not name rather than a name.
func rowsOf(t *testing.T, d *irpb.Descriptor, record string) []row {
	t.Helper()

	for _, r := range drawn(t, d).records {
		if r.name != record {
			continue
		}

		rows := make([]row, 0, len(r.items))

		for _, one := range r.items {
			rows = append(rows, row{
				item:    mermaidItem(one),
				at:      one.at.phrase(markdownCell),
				extent:  one.extent.phrase(markdownCell),
				usage:   one.usage,
				picture: one.picture,
				present: one.present.phrase(markdownCell),
			})
		}

		return rows
	}

	t.Fatalf("the document tables no record called %s", record)

	return nil
}

// tabled is one record's rows by the Item cell the document prints for each,
// which is how a test names a row without depending on where in the order it
// landed.
//
// The key is lossy on purpose and the loss is bounded by [rowCount]. Two slack
// runs at one containment path render the same cell, as do two variants and a
// group reached along two paths, so a map keyed this way keeps the last of any
// such pair — and a test that only looks up the keys it names would not notice.
// Every caller asserts the number of rows as well, so a row appearing, vanishing
// or colliding fails somewhere even when no lookup changes.
func tabled(t *testing.T, d *irpb.Descriptor, record string) map[string]row {
	t.Helper()

	ordered := rowsOf(t, d, record)

	rows := make(map[string]row, len(ordered))
	for _, one := range ordered {
		rows[one.item] = one
	}

	return rows
}

// rowCount holds a record's table to a number of rows.
//
// It is what makes the lossy key in [tabled] safe: the lookups say that the rows
// a test names are right, and this says that they are all the rows there are.
func rowCount(t *testing.T, d *irpb.Descriptor, record string, want int) {
	t.Helper()

	rows := rowsOf(t, d, record)
	if len(rows) == want {
		return
	}

	named := make([]string, 0, len(rows))
	for _, one := range rows {
		named = append(named, one.item)
	}

	t.Errorf("the table for %s holds %d rows, want %d: %v", record, len(rows), want, named)
}

// numeric and signedNumeric are a numeric picture of that many digits at that
// scale, unsigned and signed.
func numeric(digits uint32, scale int32) *irpb.Picture {
	return &irpb.Picture{Category: irpb.Category_CATEGORY_NUMERIC, Digits: digits, Scale: scale}
}

func signedNumeric(digits uint32, scale int32, at irpb.SignPosition) *irpb.Picture {
	return &irpb.Picture{
		Category:     irpb.Category_CATEGORY_NUMERIC,
		Digits:       digits,
		Scale:        scale,
		Signed:       true,
		SignPosition: at,
	}
}

// numericEditedPicture is an edited picture, which carries the digits it stores
// and none of the characters that edit them.
func numericEditedPicture(digits uint32, scale int32, sign bool) *irpb.Picture {
	return &irpb.Picture{
		Category: irpb.Category_CATEGORY_NUMERIC_EDITED,
		Digits:   digits,
		Scale:    scale,
		Signed:   sign,
	}
}

// pictureFieldNode is an elementary item stating a usage and a picture of the
// caller's choosing, which is what the fixtures about the item table need and
// [fieldNode] deliberately does not offer.
func pictureFieldNode(id uint64, original string, width uint32, usage irpb.Usage, picture *irpb.Picture) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width:   width,
		Usage:   usage,
		Picture: picture,
		Names:   &irpb.Names{Original: original},
	}}}
}

// repeatingFieldNode and repeatingGroupNode are a field and a group that repeat.
func repeatingFieldNode(id uint64, original string, width uint32, rep *irpb.Repetition) *irpb.Node {
	node := fieldNode(id, original, width)
	node.GetField().Repetition = rep

	return node
}

func repeatingGroupNode(id uint64, original string, rep *irpb.Repetition, members ...uint64) *irpb.Node {
	node := groupNode(id, original, members...)
	node.GetGroup().Repetition = rep

	return node
}

// The two counts a repetition can carry.
func constantCount(n uint32) *irpb.Repetition {
	return &irpb.Repetition{Count: &irpb.Repetition_Constant{Constant: n}}
}

func fieldCount(field uint64, min, max uint32) *irpb.Repetition {
	return &irpb.Repetition{Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
		Count:          &irpb.VariableCount_FieldId{FieldId: field},
		MinOccurrences: min,
		MaxOccurrences: max,
	}}}
}

func registerCount(register uint64, min, max uint32) *irpb.Repetition {
	return &irpb.Repetition{Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
		Count:          &irpb.VariableCount_RegisterId{RegisterId: register},
		MinOccurrences: min,
		MaxOccurrences: max,
	}}}
}

// fillerFieldNode and fillerGroupNode are an item and a group COBOL gave no
// data-name.
//
// They carry no names message at all, which is what a producer emits for a
// FILLER: an item with no data-name has no original for a substitute to stand
// beside, and the schema makes the original the member that must be present.
func fillerFieldNode(id uint64, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width:   width,
		Usage:   irpb.Usage_USAGE_DISPLAY,
		Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
	}}}
}

func fillerGroupNode(id uint64, members ...uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Group{Group: &irpb.Group{MemberIds: members}}}
}

// slackNode is a run of bytes that belongs to no item.
func slackNode(id uint64, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Slack{Slack: &irpb.Slack{Width: width}}}
}

// variantNode is an alternation, and armAt and armGroupAt are its two shapes of
// arm — a field body and a group one.
func variantNode(id uint64, arms ...*irpb.Arm) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Variant{Variant: &irpb.Variant{Arms: arms}}}
}

func armAt(predicate, field uint64) *irpb.Arm {
	return &irpb.Arm{PredicateId: predicate, Body: &irpb.Arm_FieldId{FieldId: field}}
}

func armGroupAt(predicate, group uint64) *irpb.Arm {
	return &irpb.Arm{PredicateId: predicate, Body: &irpb.Arm_GroupId{GroupId: group}}
}

// variableAutomaton is the descriptor the item tables are pinned to: two
// records, one about arithmetic and one about vocabulary.
//
// VARIABLE-RECORD is every way an offset can stop being a number — an item
// carrying no logical value, a slack run, a table whose count is a field of the
// record, a group laid out from its own offset, and a variant whose arms share
// one. PICTURE-RECORD is the other half: every USAGE this generator names, every
// category of picture it spells, every sign position, a scale in each of the
// places it can be, and a table sized by a register a transition bound from the
// record ahead of it.
//
// One descriptor rather than two so that the register count has somewhere to
// come from: docs/ir/SPEC.md requires a register count to have been bound by a
// transition taken strictly earlier than the one admitting the record, which is
// a fact about a file and not about a record.
func variableAutomaton() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),

			stateNode(2, false, 10),
			stateNode(3, true, 11),

			edgeNode(10, 100, 3, predicateAt(50), nil, []uint64{40}),
			edgeNode(11, 300, 3, predicateAt(51), nil, nil),

			integerRegister(20),
			fieldBinding(40, 20, 202),

			equalPredicate(50, 201, "\xc1"),
			equalPredicate(51, 330, "\xc2"),

			// The arms' predicates, each reading a field of the occurrence the
			// variant sits in.
			equalPredicate(60, 205, "\xc1"),
			equalPredicate(61, 205, "\xc3"),

			recordOf(100, 200, "VARIABLE-RECORD"),
			groupNode(200, "VARIABLE-RECORD", 201, 202, 203, 204, 210, 213, 214, 215, 217),
			fieldNode(201, "REC-TYPE", 1),
			numericFieldNode(202, "ENTRY-COUNT", 2, 2),
			slackNode(203, 1),

			repeatingGroupNode(204, "ENTRIES", fieldCount(202, 1, 20), 205, 206),
			fieldNode(205, "ENTRY-KIND", 1),
			variantNode(206, armGroupAt(60, 207), armAt(61, 209)),
			groupNode(207, "CASH", 208),
			pictureFieldNode(208, "CASH-AMOUNT", 3, irpb.Usage_USAGE_PACKED_DECIMAL,
				signedNumeric(5, 2, irpb.SignPosition_SIGN_POSITION_UNSPECIFIED)),
			numericFieldNode(209, "CHEQUE-NUMBER", 3, 5),

			repeatingGroupNode(210, "TRAILERS", constantCount(3), 211, 212),
			fieldNode(211, "TRAILER-TAG", 2),
			numericFieldNode(212, "TRAILER-SEQ", 2, 2),

			pictureFieldNode(213, "INDEX-SLOT", 4, irpb.Usage_USAGE_INDEX, nil),

			// A FILLER item and a FILLER group: COBOL gave neither a data-name,
			// so the producer emits no names message and the table says so in
			// its own word. A FILLER group still holds members, which is what a
			// slack node could not.
			fillerFieldNode(214, 2),
			fillerGroupNode(215, 216),
			fieldNode(216, "NOTE-CODE", 1),

			// A table of one-byte occurrences, which is the case where the
			// product in the Width cell is the count and nothing else.
			repeatingFieldNode(217, "FLAGS", 1, fieldCount(202, 1, 20)),

			recordOf(300, 350, "PICTURE-RECORD"),
			groupNode(350, "PICTURE-RECORD",
				330, 301, 302, 303, 304, 305, 306, 307, 308, 309,
				310, 311, 312, 313, 314, 315, 316, 317, 318, 319, 320, 321, 322, 323),

			fieldNode(330, "PIC-TYPE", 1),
			fieldNode(301, "PLAIN-TEXT", 6),
			pictureFieldNode(302, "NAME-TEXT", 4, irpb.Usage_USAGE_DISPLAY,
				&irpb.Picture{Category: irpb.Category_CATEGORY_ALPHABETIC}),
			pictureFieldNode(303, "EDIT-NAME", 5, irpb.Usage_USAGE_DISPLAY,
				&irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC_EDITED}),
			pictureFieldNode(304, "EDIT-TOTAL", 9, irpb.Usage_USAGE_DISPLAY, numericEditedPicture(5, 2, false)),
			pictureFieldNode(305, "UNSIGNED-AMT", 4, irpb.Usage_USAGE_DISPLAY, numeric(4, 0)),
			pictureFieldNode(306, "LEAD-SIGN", 3, irpb.Usage_USAGE_DISPLAY,
				signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_LEADING)),
			pictureFieldNode(307, "TRAIL-SIGN", 3, irpb.Usage_USAGE_DISPLAY,
				signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_TRAILING)),
			pictureFieldNode(308, "LEAD-SEP-SIGN", 4, irpb.Usage_USAGE_DISPLAY,
				signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_LEADING_SEPARATE)),
			pictureFieldNode(309, "TRAIL-SEP-SIGN", 4, irpb.Usage_USAGE_DISPLAY,
				signedNumeric(3, 0, irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE)),
			pictureFieldNode(310, "SCALED-AMT", 4, irpb.Usage_USAGE_PACKED_DECIMAL,
				signedNumeric(7, 2, irpb.SignPosition_SIGN_POSITION_UNSPECIFIED)),
			pictureFieldNode(311, "HIGH-SCALE", 2, irpb.Usage_USAGE_PACKED_DECIMAL, numeric(2, 5)),
			pictureFieldNode(312, "LOW-SCALE", 2, irpb.Usage_USAGE_PACKED_DECIMAL, numeric(3, -2)),
			pictureFieldNode(313, "ALL-SCALE", 3, irpb.Usage_USAGE_DISPLAY, numeric(3, 3)),
			pictureFieldNode(314, "COMP6-COUNT", 2, irpb.Usage_USAGE_COMP_6, numeric(4, 0)),
			pictureFieldNode(315, "BIN-COUNT", 2, irpb.Usage_USAGE_BINARY, numeric(4, 0)),
			pictureFieldNode(316, "NATIVE-COUNT", 4, irpb.Usage_USAGE_COMP_5, numeric(9, 0)),
			pictureFieldNode(317, "SHORT-FLOAT", 4, irpb.Usage_USAGE_COMP_1, nil),
			pictureFieldNode(318, "LONG-FLOAT", 8, irpb.Usage_USAGE_COMP_2, nil),
			pictureFieldNode(319, "IDX", 4, irpb.Usage_USAGE_INDEX, nil),
			pictureFieldNode(320, "PTR", 4, irpb.Usage_USAGE_POINTER, nil),
			pictureFieldNode(321, "NAT-TEXT", 8, irpb.Usage_USAGE_NATIONAL, nil),

			repeatingFieldNode(322, "TAGS", 2, constantCount(4)),
			repeatingGroupNode(323, "EXTRAS", registerCount(20, 0, 9), 324),
			fieldNode(324, "EXTRA-TEXT", 3),
		},
	}
}
