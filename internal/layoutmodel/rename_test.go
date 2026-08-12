// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// renamesOf is the whole pipeline a caller runs: parse the source, then read the
// renames out of it.
func renamesOf(t *testing.T, source string) ([]Rename, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadRenames(file)
}

// renaming wraps rename forms in the least a layout has to say for them to be
// readable: one `record` form per record they are rooted at.
//
// The records come first and one to a line, so that the first form after them
// always starts at line len(records)+1 and every position in it is the column it
// was written at.
func renaming(records []string, forms ...string) string {
	lines := make([]string, 0, len(records)+len(forms))

	for _, record := range records {
		lines = append(lines, fmt.Sprintf("(record %s (copybook \"cpy/x.cpy\" %s-REC))", record, record))
	}

	return strings.Join(append(lines, forms...), "\n")
}

// renderRenames draws the layer the way the tests assert one: every rename in
// source order, the item it is about, both of that item's names and where the
// substitute was written.
//
// It is a rendering rather than a struct literal for [renderDiscrimination]'s
// reason: what has to be right is the model *and* the position of every part of
// it, and a rendering carrying both fails with the whole model in the message.
func renderRenames(renames []Rename) string {
	lines := make([]string, 0, len(renames))

	for _, rename := range renames {
		lines = append(lines, fmt.Sprintf(
			"%s rename %s: %s -> %s at %s",
			rename.Pos, rename.Item, quote(rename.Original()), quote(rename.Substitute), rename.SubstitutePos,
		))
	}

	return strings.Join(lines, "\n")
}

// TestReadRenamesModelsTheLayer is the criterion this reader exists for: a
// layout's renames become typed values naming their targets in full, each
// carrying the substitute beside the name the copybook gave the item.
func TestReadRenamesModelsTheLayer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a layout that renames nothing",
			source: renaming([]string{"DETAIL"}),
			want:   nil,
		},
		{
			name:   "a field renamed for a generator",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-KEY D-CUST-NO) "CustomerNumber")`),
			want: []string{
				`layout.sexpr:2:1 rename (item DETAIL D-KEY D-CUST-NO): "D-CUST-NO" -> "CustomerNumber" at layout.sexpr:2:39`,
			},
		},
		{
			// A reference is rooted at a record and never at a copybook path, so
			// two records over one `01`-level are renamed independently and
			// neither reaches the other. The two substitutes below would collide
			// if they did.
			name: "two records over one copybook item, renamed independently",
			source: renaming([]string{"ORDER-OPEN", "ORDER-CLOSE"},
				`(rename (item ORDER-OPEN O-KEY O-NO) "OrderNumber")`,
				`(rename (item ORDER-CLOSE O-KEY O-NO) "OrderNumber")`,
			),
			want: []string{
				`layout.sexpr:3:1 rename (item ORDER-OPEN O-KEY O-NO): "O-NO" -> "OrderNumber" at layout.sexpr:3:38`,
				`layout.sexpr:4:1 rename (item ORDER-CLOSE O-KEY O-NO): "O-NO" -> "OrderNumber" at layout.sexpr:4:39`,
			},
		},
		{
			// One name under two parents is two names, and neither is the other's
			// sibling. Nothing here is a collision.
			name: "one name substituted under two parents",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-BILL-TO D-NAME) "Name")`,
				`(rename (item DETAIL D-SHIP-TO D-NAME) "Name")`,
			),
			want: []string{
				`layout.sexpr:2:1 rename (item DETAIL D-BILL-TO D-NAME): "D-NAME" -> "Name" at layout.sexpr:2:40`,
				`layout.sexpr:3:1 rename (item DETAIL D-SHIP-TO D-NAME): "D-NAME" -> "Name" at layout.sexpr:3:40`,
			},
		},
		{
			// A rename substituting the name the item already carries says the
			// same thing twice. It leaves every name where it was, and an item is
			// not its own sibling.
			name:   "a rename substituting the item's own name",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-CUST-NO) "D-CUST-NO")`),
			want: []string{
				`layout.sexpr:2:1 rename (item DETAIL D-CUST-NO): "D-CUST-NO" -> "D-CUST-NO" at layout.sexpr:2:33`,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := renamesOf(t, testCase.source)
			if err != nil {
				t.Fatalf("the reader rejects %s: %v", testCase.source, err)
			}

			if got, want := renderRenames(read), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("the reader reads\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestRenamesReachGroupsAndOccurrences is the half of the layer that is a
// deliberate absence of a rule.
//
// docs/layout/SPEC.md admits a reference naming a group and a reference naming
// an item that repeats or sits inside one, in which case the substitute is the
// name of the item and reaches every occurrence of it. Which of those a given
// path is needs the copybook, so a reader that rejected any depth or any shape
// would be rejecting layouts on a guess.
func TestRenamesReachGroupsAndOccurrences(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		item string
	}{
		{name: "an elementary item under the record", item: "(item POLICY PL-NUMBER)"},
		{name: "a group under the record", item: "(item POLICY PL-ENTRIES)"},
		{name: "a field inside a repeating group", item: "(item POLICY PL-ENTRIES PL-KIND)"},
		{name: "a group inside a repeating group", item: "(item POLICY PL-ENTRIES PL-BODY-MOTOR)"},
		{name: "a field inside a group inside a repeating group", item: "(item POLICY PL-ENTRIES PL-BODY-MOTOR PL-REG)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := renamesOf(t, renaming([]string{"POLICY"}, fmt.Sprintf("(rename %s \"Substitute\")", testCase.item)))
			if err != nil {
				t.Fatalf("the reader rejects a rename on %s: %v", testCase.item, err)
			}

			if len(read) != 1 {
				t.Fatalf("the reader reads %d renames, want 1", len(read))
			}

			if got := read[0].Item.String(); got != testCase.item {
				t.Errorf("the rename is on %s, want %s", got, testCase.item)
			}
		})
	}
}

// TestSubstitutesAreCarriedVerbatim is docs/layout/SPEC.md's "no implementation
// MAY apply the casing or identifier conventions of any language to it".
//
// The substitute is a string in a file every generator reads, so anything done
// to it here is done to it in every target language. Each of the names below is
// one some language would munge and another would keep, and the reader carries
// all of them through unchanged.
func TestSubstitutesAreCarriedVerbatim(t *testing.T) {
	t.Parallel()

	substitutes := []string{
		"CustomerNumber",
		"customerNumber",
		"customer_number",
		"customer-number",
		"customer number",
		"CUSTOMER NUMBER",
		"Kundennummer",
		"9LIVES",
		"type",
		" leading and trailing ",
	}

	for _, substitute := range substitutes {
		t.Run(substitute, func(t *testing.T) {
			t.Parallel()

			source := renaming([]string{"DETAIL"}, fmt.Sprintf("(rename (item DETAIL D-CUST-NO) %q)", substitute))

			read, err := renamesOf(t, source)
			if err != nil {
				t.Fatalf("the reader rejects %s: %v", source, err)
			}

			if len(read) != 1 {
				t.Fatalf("the reader reads %d renames, want 1", len(read))
			}

			if read[0].Substitute != substitute {
				t.Errorf("the substitute is carried as %q, want %q", read[0].Substitute, substitute)
			}

			if read[0].Original() != "D-CUST-NO" {
				t.Errorf("the original is %q, want \"D-CUST-NO\"; a substitute stands beside it", read[0].Original())
			}
		})
	}
}

// TestReadRenamesRejects is the load-bearing half: a reader that accepts
// everything passes every test above.
//
// Each case is a layout the format does not admit, and the whole joined message
// is asserted rather than the first fault, because reporting every fault is the
// reader's other requirement.
func TestReadRenamesRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			// A bare name in the target position is a record name, so a bare
			// name that is not one is a rename on a record that is not there —
			// and never an item named without a reference. Duplicate data names
			// are legal COBOL, so a bare name is still not an identity for an
			// *item*, and the format has no spelling for one.
			name:   "a bare name that is not a record the layout defines",
			source: renaming([]string{"DETAIL"}, `(rename D-CUST-NO "CustomerNumber")`),
			want: []string{
				"layout.sexpr:2:9: form \"rename\" names record \"D-CUST-NO\", and the layout defines no " +
					"record of that name",
			},
		},
		{
			name:   "a target naming a record and no item below it",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL) "CustomerNumber")`),
			want: []string{
				"layout.sexpr:2:9: an item reference is written (item <record-name> <name> ...), and this is " +
					"a reference naming a record and no item below it",
			},
		},
		{
			name:   "a target rooted at a record nobody defined",
			source: renaming([]string{"DETAIL"}, `(rename (item HEADER H-CUST-NO) "CustomerNumber")`),
			want: []string{
				"layout.sexpr:2:9: form \"rename\" names record \"HEADER\", and the layout defines no record of " +
					"that name",
			},
		},
		{
			name:   "a rename carrying no name",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-CUST-NO))`),
			want: []string{
				"layout.sexpr:2:1: a rename is written (rename <item-ref> \"<name>\") or (rename <record-name> \"<name>\"), and this has a target and " +
					"no name",
			},
		},
		{
			name:   "a rename carrying no target",
			source: renaming([]string{"DETAIL"}, `(rename "CustomerNumber")`),
			want: []string{
				"layout.sexpr:2:1: a rename is written (rename <item-ref> \"<name>\") or (rename <record-name> \"<name>\"), and this has a name and " +
					"no target",
			},
		},
		{
			name:   "a rename carrying two names",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-CUST-NO) "CustomerNumber" "CustNo")`),
			want: []string{
				"layout.sexpr:2:1: a rename is written (rename <item-ref> \"<name>\") or (rename <record-name> \"<name>\"), and this has several",
			},
		},
		{
			// The substitute is text because it is a name and not a symbol: a
			// name a target language would spell with a space in it is one this
			// format has to be able to carry.
			name:   "a name written as a symbol",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-CUST-NO) CustomerNumber)`),
			want: []string{
				"layout.sexpr:2:33: a rename is written (rename <item-ref> \"<name>\") or (rename <record-name> \"<name>\"), and this has the symbol " +
					"\"CustomerNumber\"",
			},
		},
		{
			name:   "a rename substituting no name at all",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-CUST-NO) "")`),
			want: []string{
				"layout.sexpr:2:33: the rename on (item DETAIL D-CUST-NO) substitutes a name of no characters, " +
					"and a name is at least one",
			},
		},
		{
			name: "one item renamed twice",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-CUST-NO) "CustomerNumber")`,
				`(rename (item DETAIL D-CUST-NO) "CustNo")`,
			),
			want: []string{
				"layout.sexpr:3:1: (item DETAIL D-CUST-NO) is renamed twice, and is renamed first at " +
					"layout.sexpr:2:1; a rename names an item at most once",
			},
		},
		{
			// The two are one statement made twice, which is what the duplicate
			// rule is about. A collision message here would name one item on
			// both sides of itself and read as though two were involved.
			name: "one item renamed twice to one name",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-CUST-NO) "CustomerNumber")`,
				`(rename (item DETAIL D-CUST-NO) "CustomerNumber")`,
			),
			want: []string{
				"layout.sexpr:3:1: (item DETAIL D-CUST-NO) is renamed twice, and is renamed first at " +
					"layout.sexpr:2:1; a rename names an item at most once",
			},
		},
		{
			name: "one name substituted for two items under one parent",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-KEY D-CUST-NO) "Customer")`,
				`(rename (item DETAIL D-KEY D-CUST-NAME) "Customer")`,
			),
			want: []string{
				"layout.sexpr:3:41: name \"Customer\" is substituted for (item DETAIL D-KEY D-CUST-NO) and for " +
					"(item DETAIL D-KEY D-CUST-NAME), which stand under one parent, and is substituted first at " +
					"layout.sexpr:2:39",
			},
		},
		{
			// The sibling is an item the layout itself names, which is what makes
			// the collision answerable without reading the copybook.
			name: "a substitute equal to the name of a sibling the layout names",
			source: renaming([]string{"DETAIL"},
				`(discriminate DETAIL (equals (item DETAIL D-KEY D-REC-TYPE) "20"))`,
				`(rename (item DETAIL D-KEY D-CUST-NO) "D-REC-TYPE")`,
			),
			want: []string{
				"layout.sexpr:3:39: the rename on (item DETAIL D-KEY D-CUST-NO) substitutes \"D-REC-TYPE\", " +
					"which is the name of (item DETAIL D-KEY D-REC-TYPE) beside it at layout.sexpr:2:30; an " +
					"original is carried beside a substitute rather than in place of it, so the item there " +
					"answers to that name still",
			},
		},
		{
			// A swap is two collisions and not a pair that cancels: an original
			// is carried beside a substitute, so both items answer to both names.
			name: "two siblings whose names are swapped",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-KEY D-ONE) "D-TWO")`,
				`(rename (item DETAIL D-KEY D-TWO) "D-ONE")`,
			),
			want: []string{
				"layout.sexpr:2:35: the rename on (item DETAIL D-KEY D-ONE) substitutes \"D-TWO\", which is the " +
					"name of (item DETAIL D-KEY D-TWO) beside it at layout.sexpr:3:9; an original is carried " +
					"beside a substitute rather than in place of it, so the item there answers to that name still",
				"layout.sexpr:3:35: the rename on (item DETAIL D-KEY D-TWO) substitutes \"D-ONE\", which is the " +
					"name of (item DETAIL D-KEY D-ONE) beside it at layout.sexpr:2:9; an original is carried " +
					"beside a substitute rather than in place of it, so the item there answers to that name still",
			},
		},
		{
			// Every fault the form carries is reported: a rename rooted at a
			// record nobody defined and substituting a name its target's sibling
			// carries is two things to fix rather than one to discover on the
			// next run.
			name: "a rename wrong in two ways at once",
			source: renaming([]string{"DETAIL"},
				`(rename (item HEADER H-KEY H-ONE) "H-TWO")`,
				`(rename (item HEADER H-KEY H-TWO) "H-THREE")`,
			),
			want: []string{
				"layout.sexpr:2:9: form \"rename\" names record \"HEADER\", and the layout defines no record of " +
					"that name",
				"layout.sexpr:2:35: the rename on (item HEADER H-KEY H-ONE) substitutes \"H-TWO\", which is the " +
					"name of (item HEADER H-KEY H-TWO) beside it at layout.sexpr:3:9; an original is carried " +
					"beside a substitute rather than in place of it, so the item there answers to that name still",
				"layout.sexpr:3:9: form \"rename\" names record \"HEADER\", and the layout defines no record of " +
					"that name",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := renamesOf(t, testCase.source)
			if err == nil {
				t.Fatalf("the reader accepts %s", testCase.source)
			}

			if read != nil {
				t.Errorf("the reader hands back renames beside the fault: %+v", read)
			}

			if got, want := err.Error(), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("the reader reports\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestRenameFaultsAreAssertable is the other requirement on a fault: a caller
// deciding what to do about one reaches for the type rather than for the text of
// the message.
func TestRenameFaultsAreAssertable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name:   "a bare name naming no record is a record fault",
			source: renaming([]string{"DETAIL"}, `(rename D-CUST-NO "CustomerNumber")`),
			assert: func(t *testing.T, err error) {
				var fault *UnknownRecordError
				if !errors.As(err, &fault) {
					t.Fatalf("no UnknownRecordError in %v", err)
				}
			},
		},
		{
			name:   "a reference that is not one is still a reference fault",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL) "CustomerNumber")`),
			assert: func(t *testing.T, err error) {
				var fault *ItemReferenceError
				if !errors.As(err, &fault) {
					t.Fatalf("no ItemReferenceError in %v", err)
				}
			},
		},
		{
			name: "a second rename on one item names both statements",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-CUST-NO) "CustomerNumber")`,
				`(rename (item DETAIL D-CUST-NO) "CustNo")`,
			),
			assert: func(t *testing.T, err error) {
				var fault *DuplicateRenameError
				if !errors.As(err, &fault) {
					t.Fatalf("no DuplicateRenameError in %v", err)
				}

				if fault.Item.String() != "(item DETAIL D-CUST-NO)" || fault.First.Line != 2 || fault.Pos.Line != 3 {
					t.Errorf("the fault is on %s at %s, first at %s", fault.Item, fault.Pos, fault.First)
				}
			},
		},
		{
			name: "a collision names both items and the name they share",
			source: renaming([]string{"DETAIL"},
				`(rename (item DETAIL D-KEY D-CUST-NO) "Customer")`,
				`(rename (item DETAIL D-KEY D-CUST-NAME) "Customer")`,
			),
			assert: func(t *testing.T, err error) {
				var fault *RenameCollisionError
				if !errors.As(err, &fault) {
					t.Fatalf("no RenameCollisionError in %v", err)
				}

				if fault.Name != "Customer" {
					t.Errorf("the fault is over %q, want \"Customer\"", fault.Name)
				}

				if fault.Items[0].Name() != "D-CUST-NO" || fault.Items[1].Name() != "D-CUST-NAME" {
					t.Errorf("the fault is over %s and %s, want the two items in source order", fault.Items[0], fault.Items[1])
				}
			},
		},
		{
			name: "a shadowed sibling names the item that already carries the name",
			source: renaming([]string{"DETAIL"},
				`(discriminate DETAIL (equals (item DETAIL D-KEY D-REC-TYPE) "20"))`,
				`(rename (item DETAIL D-KEY D-CUST-NO) "D-REC-TYPE")`,
			),
			assert: func(t *testing.T, err error) {
				var fault *RenameShadowsSiblingError
				if !errors.As(err, &fault) {
					t.Fatalf("no RenameShadowsSiblingError in %v", err)
				}

				if fault.Sibling.String() != "(item DETAIL D-KEY D-REC-TYPE)" || fault.First.Line != 2 {
					t.Errorf("the fault names %s at %s, want the discriminator's target", fault.Sibling, fault.First)
				}
			},
		},
		{
			name:   "a rename rooted at an undefined record names the record",
			source: renaming([]string{"DETAIL"}, `(rename (item HEADER H-CUST-NO) "CustomerNumber")`),
			assert: func(t *testing.T, err error) {
				var fault *UnknownRecordError
				if !errors.As(err, &fault) {
					t.Fatalf("no UnknownRecordError in %v", err)
				}

				if fault.Record != "HEADER" || fault.Form != tagRename {
					t.Errorf("the fault is on record %q under form %q, want \"HEADER\" under %q", fault.Record, fault.Form, tagRename)
				}
			},
		},
		{
			name:   "an empty substitute names the item it was written for",
			source: renaming([]string{"DETAIL"}, `(rename (item DETAIL D-CUST-NO) "")`),
			assert: func(t *testing.T, err error) {
				var fault *EmptyRenameError
				if !errors.As(err, &fault) {
					t.Fatalf("no EmptyRenameError in %v", err)
				}

				if fault.Item.String() != "(item DETAIL D-CUST-NO)" {
					t.Errorf("the fault is on %s, want (item DETAIL D-CUST-NO)", fault.Item)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := renamesOf(t, testCase.source)
			if err == nil {
				t.Fatal("read without a fault, want one")
			}

			testCase.assert(t, err)
		})
	}
}

// TestTheSpecsWorkedExampleRenames is the staleness gate over the layer.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is read out of the document rather than
// copied here for [TestTheSpecsWorkedExampleDiscriminates]'s reason: a rename
// the example writes and this reader does not read would otherwise be invisible
// until somebody pasted the example into a file.
func TestTheSpecsWorkedExampleRenames(t *testing.T) {
	t.Parallel()

	read, err := renamesOf(t, specExample(t))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own worked example: %v", err)
	}

	if len(read) != 1 {
		t.Fatalf("the example carries %d renames, want 1", len(read))
	}

	if got := read[0].Item.String(); got != "(item ORDER-HEADER OH-KEY OH-CUST-NO)" {
		t.Errorf("the rename is on %s, want (item ORDER-HEADER OH-KEY OH-CUST-NO)", got)
	}

	if read[0].Substitute != "CustomerNumber" || read[0].Original() != "OH-CUST-NO" {
		t.Errorf("the example renames %q to %q, want \"OH-CUST-NO\" to \"CustomerNumber\"", read[0].Original(), read[0].Substitute)
	}
}

// TestARenameMayNameARecord is docs/layout/SPEC.md's "A rename may name a
// record": the target position takes a bare record name as well as a reference,
// and which of the two was written is decided by its shape.
func TestARenameMayNameARecord(t *testing.T) {
	t.Parallel()

	read, err := renamesOf(t, renaming([]string{"PURCHASE", "REFUND"},
		`(rename PURCHASE "TXN-PURCHASE-REC")`,
		`(rename (item REFUND R-KEY) "RefundKey")`,
	))
	if err != nil {
		t.Fatalf("renames: %v", err)
	}

	if len(read) != 2 {
		t.Fatalf("the layout carries %d renames, want 2", len(read))
	}

	if !read[0].NamesRecord() || read[0].Record != "PURCHASE" {
		t.Errorf("the first rename names %+v, want record PURCHASE", read[0])
	}

	// A record rename has no original here. The name it stands beside is the
	// record's `01`-level, which the `copybook` child names and this model does
	// not carry.
	if read[0].Original() != "" {
		t.Errorf("a record rename carries the original %q", read[0].Original())
	}

	if read[1].NamesRecord() {
		t.Errorf("the second rename reads as a record rename: %+v", read[1])
	}
}

// TestReadRenamesRejectsRecordRenames is the collision rule read over record
// types: a record node has no parent, so what is checked is the two halves that
// are still decidable from the layout alone.
func TestReadRenamesRejectsRecordRenames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "one record renamed twice",
			source: renaming([]string{"PURCHASE"},
				`(rename PURCHASE "One")`,
				`(rename PURCHASE "Two")`,
			),
			want: "layout.sexpr:3:1: record \"PURCHASE\" is renamed twice, and is renamed first at " +
				"layout.sexpr:2:1; a rename names a record at most once",
		},
		{
			name: "one name substituted for two records",
			source: renaming([]string{"PURCHASE", "REFUND"},
				`(rename PURCHASE "Txn")`,
				`(rename REFUND   "Txn")`,
			),
			want: "layout.sexpr:4:18: name \"Txn\" is substituted for records \"PURCHASE\" and \"REFUND\", " +
				"and is substituted first at layout.sexpr:3:18",
		},
		{
			name: "a substitute another record is bound to",
			source: renaming([]string{"PURCHASE", "REFUND"},
				`(rename PURCHASE "REFUND-REC")`,
			),
			want: "layout.sexpr:3:18: name \"REFUND-REC\" is substituted for record \"PURCHASE\", and record " +
				"\"REFUND\" is bound to the item of that name at layout.sexpr:2:38",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := renamesOf(t, testCase.source)
			if err == nil {
				t.Fatalf("the reader accepts %s", testCase.source)
			}

			if read != nil {
				t.Errorf("the reader hands back renames beside the fault: %+v", read)
			}

			if got := err.Error(); got != testCase.want {
				t.Errorf("the reader reports\n%s\nwant\n%s", got, testCase.want)
			}
		})
	}
}

// TestARecordRenamedToItsOwnBindingIsAdmitted is the "not its own collision"
// half, which the sibling rules have too: a rename substituting the name its own
// record is already bound to says the same thing twice and leaves every name
// where it was.
func TestARecordRenamedToItsOwnBindingIsAdmitted(t *testing.T) {
	t.Parallel()

	read, err := renamesOf(t, renaming([]string{"PURCHASE"}, `(rename PURCHASE "PURCHASE-REC")`))
	if err != nil {
		t.Fatalf("the reader refuses a record renamed to its own binding: %v", err)
	}

	if len(read) != 1 {
		t.Fatalf("the layout carries %d renames, want 1", len(read))
	}
}
