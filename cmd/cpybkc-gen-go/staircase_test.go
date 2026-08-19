// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/codec"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/assemble"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/resolve"
	"github.com/Zaba505/cpybkc/irpb"
)

// This file is the whole of #293's assertion, and it is the only test in this
// repository that runs the *producer* — the copybook reader, `resolve` and
// `assemble` — and this *consumer* in one process. Everything else here builds a
// descriptor by hand, which is the right shape for a test about what a generator
// emits and the wrong one for a test about whether two independent width
// staircases agree: a hand-written width is a third answer, and three answers
// that agree because one person typed them all is not agreement.
//
// It is `copybook`'s TestBinarySizeAgreesWithCodec one repository out. Upstream,
// the two sides are `copybook`'s own table and `codec`'s, and the test exists
// because the two cannot share a type. Here the two sides are the width
// `resolve` put in the IR and the width the generated package's Encoding makes
// `codec` read, and the test exists because the staircase crosses two package
// boundaries and a protobuf enum on the way — `copybook.BinarySize` becomes
// `irpb.BinarySize` in `assemble` and `codec.BinarySize` here, and each hop is
// a table somebody could get wrong in a way nothing else would notice.
//
// # Why a wrong answer here is invisible everywhere else
//
// A binary item's width is a staircase in its digit count, so the wrong
// staircase does not corrupt the item it governs: it reads a plausible number
// out of the wrong number of bytes and leaves every field behind it at the
// wrong offset. The record is still the length the dataset says, every
// alphanumeric field still holds characters, and a round trip through the
// generated package still succeeds — because the same wrong staircase writes
// the bytes back. Nothing short of comparing against the producer's own widths
// can see it, which is what this file does.

// staircaseCase is which of the two forms of a COMP item a case is about, and
// it is a parameter rather than a constant because "the width does not depend
// on the S" is a claim rather than an axiom.
//
// It is not obviously true of `1--8`, which `codec/SPEC.md` defines as the
// smallest byte count whose **signed** range holds the digits: a side that
// sized an unsigned item from its unsigned range instead would give a 7-digit
// item three bytes where the other gives it four, and every case here would
// still pass if only signed items were compared. So both are run, and the
// claim is a result rather than a premise.
type staircaseCase struct {
	picture string
	sign    codec.Signedness
}

// staircaseCases is both forms, named as a PICTURE distinguishes them.
var staircaseCases = map[string]staircaseCase{
	"signed":   {picture: "S9", sign: codec.Signed},
	"unsigned": {picture: "9", sign: codec.Unsigned},
}

// staircaseCopybook is one COMP item at the digit count under test, which is
// the smallest record that has a binary width at all.
func staircaseCopybook(of staircaseCase, digits int) string {
	return fmt.Sprintf("01 BIN-REC.\n   05 B-VALUE PIC %s(%d) COMP.\n", of.picture, digits)
}

// staircaseLayout is that record on its own, under a descriptor-word framing.
//
// Descriptor-word rather than the unframed form the corpus uses because
// `lrecl` is an *exact* bound there: every record type has to account for all
// of it, and the extent of this one record is the very thing under test, so an
// unframed layout would need its `lrecl` computed from the staircase — the
// answer being asserted, supplied as an input. Under a descriptor-word framing
// the bound is a maximum, and sixty-four bytes is above every staircase's
// widest step.
const staircaseLayout = `(framing (recfm V) (lrecl 64))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record BIN (copybook "bin.cpy" BIN-REC))
(discriminate BIN single-record-type)
(sequence (* BIN))`

// staircaseDialect is IBM Enterprise COBOL's layout with the staircase replaced.
//
// The other four members are held constant on purpose: SYNCHRONIZED, the
// REDEFINES rule and the two item widths do not touch a COMP item's own width,
// and varying them would put a second reason in the way of a failure.
func staircaseDialect(binary copybook.BinarySize) copybook.Dialect {
	dialect := copybook.IBMEnterprise()
	dialect.Binary = binary

	return dialect
}

// resolvedUnder is the descriptor this repository produces for one COMP item of
// the given digit count, laid out under the given staircase.
//
// It drives the real readers and the real resolver, exactly as
// internal/assemble's own tests do, so that what comes back is a descriptor the
// shipped pipeline would emit rather than one assembled to suit the assertion.
func resolvedUnder(t *testing.T, binary copybook.BinarySize, of staircaseCase, digits int) *irpb.Descriptor {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(staircaseLayout))
	if err != nil {
		t.Fatalf("parsing the layout: %v", err)
	}

	framing, err := layoutmodel.ReadFraming(file)
	if err != nil {
		t.Fatalf("reading the framing layer: %v", err)
	}

	profile, err := layoutmodel.ReadProfile(file)
	if err != nil {
		t.Fatalf("reading the encoding layer: %v", err)
	}

	sequence, err := layoutmodel.ReadSequence(file)
	if err != nil {
		t.Fatalf("reading the sequencing layer: %v", err)
	}

	discrimination, err := layoutmodel.ReadDiscrimination(file)
	if err != nil {
		t.Fatalf("reading the discrimination layer: %v", err)
	}

	parsed, err := cobol.Parse(strings.NewReader(staircaseCopybook(of, digits)), cobol.WithFragment())
	if err != nil {
		t.Fatalf("parsing the copybook: %v", err)
	}

	records, err := copybook.Build(parsed.Fragment.Entries)
	if err != nil {
		t.Fatalf("building the copybook: %v", err)
	}

	dialect := staircaseDialect(binary)

	resolved, err := resolve.Resolve(records[0], resolve.Options{
		Copybook: "bin.cpy",
		Dialect:  dialect,
		Framing:  framing,
		Reading:  layoutmodel.ODOSlide,
		Encoding: profile.Axes,
	})
	if err != nil {
		t.Fatalf("resolving under %s at %d digits: %v", binary, digits, err)
	}

	automaton, err := resolve.CompileSequence(resolve.Sequencing{
		Sequence: sequence,
		Dialect:  dialect,
		Reading:  layoutmodel.ODOSlide,
		Encoding: profile.Axes,
		Records: []resolve.SequencedRecord{{
			Name:          "BIN",
			Copybook:      "bin.cpy",
			Item:          records[0],
			Discriminator: discrimination.Records[0].Strategy,
		}},
	})
	if err != nil {
		t.Fatalf("compiling the sequence: %v", err)
	}

	descriptor, err := assemble.Assemble(assemble.Options{
		Framing:   framing,
		Automaton: automaton,
		Records:   []assemble.Record{{Name: "BIN", Copybook: "bin.cpy", Resolved: resolved[0]}},
	})
	if err != nil {
		t.Fatalf("assembling under %s at %d digits: %v", binary, digits, err)
	}

	return descriptor
}

// binaryFieldOf is the descriptor's one field node.
func binaryFieldOf(t *testing.T, d *irpb.Descriptor) *irpb.Field {
	t.Helper()

	for _, node := range d.GetNodes() {
		if field := node.GetField(); field != nil {
			return field
		}
	}

	t.Fatal("the descriptor carries no field node")

	return nil
}

// codecWidthOf is the number of bytes codec gives a binary item of that digit
// count under the encoding the descriptor resolved.
//
// Measured through the **public** Writer, as upstream's mirror measures its
// half: what is compared is the bytes a caller's file actually gets, not a
// table that could agree while the byte paths do not. [encodingValue] is the
// same function the generated tests are laid out with, so this is the width the
// generated package reads at and not a second reading of the axis.
func codecWidthOf(t *testing.T, enc *irpb.Encoding, of staircaseCase, digits int) int {
	t.Helper()

	value, err := encodingValue(enc)
	if err != nil {
		t.Fatalf("reading the descriptor's encoding: %v", err)
	}

	w, err := codec.NewBytesWriter(nil, value)
	if err != nil {
		t.Fatalf("codec.NewBytesWriter: %v", err)
	}

	if err := w.WriteBinaryBig(big.NewInt(0), digits, of.sign); err != nil {
		t.Fatalf("writing a %d-digit binary item: %v", digits, err)
	}

	return int(w.Offset())
}

// staircases is every staircase this repository can resolve under: `copybook`'s
// declared members, which is also what [copybook.Dialect.Validate] admits.
//
// All four rather than the one both `dialect()` functions currently hardcode.
// docs/ir/SPEC.md's requirement is that a descriptor's binary widths are the
// ones its consumer reads, and that has to hold for every staircase a producer
// can put in the IR — not only for the one this build happens to pick today,
// which is the whole shape of the failure #293 was filed about.
var staircases = []copybook.BinarySize{
	copybook.BinarySize248,
	copybook.BinarySize1248,
	copybook.BinarySizeSmallest,
	copybook.BinarySizeFull,
}

// TestTheResolvedBinaryWidthIsTheOneCodecReads is #293's acceptance criterion:
// the width `resolve` computed and the width the generated package's Encoding
// makes `codec` read are the same number, at every digit count and under every
// staircase.
//
// It fails if either side moves. A staircase changed in `copybook` moves the
// IR's width; a staircase changed in `codec`, or a hop mismapped in
// [binarySizeOf] or [binaryValue], moves what the Writer occupies. Nothing in
// the descriptor, in the generated code or in a round trip through it disagrees
// with either on its own.
func TestTheResolvedBinaryWidthIsTheOneCodecReads(t *testing.T) {
	t.Parallel()

	for _, binary := range staircases {
		for name, of := range staircaseCases {
			t.Run(binary.String()+"/"+name, func(t *testing.T) {
				t.Parallel()

				// Every digit count a PICTURE may declare, not a sample of
				// them: the staircases differ at their boundaries, and a
				// sample is how a boundary moves without anything noticing.
				//
				// Thirty-one rather than eighteen, and the extra thirteen are
				// not padding. Every staircase has a step past eighteen digits
				// — sixteen bytes, which IBM reaches only under ARITH(EXTEND)
				// — and it is the last boundary each of them has. A loop
				// stopping at eighteen would be the one that never compares
				// the widest step, where a disagreement is eight bytes rather
				// than one.
				for digits := 1; digits <= maxBinaryDigits; digits++ {
					d := resolvedUnder(t, binary, of, digits)
					field := binaryFieldOf(t, d)

					resolvedWidth := int(field.GetWidth())
					if got := codecWidthOf(t, field.GetEncoding(), of, digits); got != resolvedWidth {
						t.Errorf("a %d-digit %s COMP item resolves to %d bytes under %s and codec reads it as %d",
							digits, name, resolvedWidth, binary, got)
					}
				}
			})
		}
	}
}

// maxBinaryDigits is the most digits a PICTURE this repository reads may
// declare, which is COBOL's thirty-one.
//
// It is the loop bound above rather than eighteen because eighteen is where the
// staircases stop *differing from each other* and not where they stop: all four
// give nineteen digits and beyond sixteen bytes, and that step is a boundary
// like any other.
const maxBinaryDigits = 31

// TestASignedTwoDigitCompItemIsTheRowTheStaircasesForkOn is the case the loop
// above would cover and nobody would notice it had stopped covering.
//
// One digit or two is the only row where the declared staircases disagree by a
// single byte: IBM Enterprise COBOL gives `PIC S9(2) COMP` two bytes and
// GnuCOBOL's default gives it one. Every other disagreement between them is
// either wider than a byte or at a digit count nobody writes by accident, so
// this is the row an off-by-one hides in — and a one-byte shift is the one that
// still leaves the following field looking like a field.
//
// The widths are written out here rather than derived, which is the one place
// in this file that is deliberate: the loop above proves the two sides agree,
// and this proves they agree on the *right* number rather than on each other's
// mistake.
func TestASignedTwoDigitCompItemIsTheRowTheStaircasesForkOn(t *testing.T) {
	t.Parallel()

	for binary, want := range map[copybook.BinarySize]int{
		copybook.BinarySize248:      2,
		copybook.BinarySize1248:     1,
		copybook.BinarySizeSmallest: 1,
		copybook.BinarySizeFull:     8,
	} {
		t.Run(binary.String(), func(t *testing.T) {
			t.Parallel()

			signed := staircaseCases["signed"]
			field := binaryFieldOf(t, resolvedUnder(t, binary, signed, 2))

			if got := int(field.GetWidth()); got != want {
				t.Errorf("PIC S9(2) COMP resolves to %d bytes under %s, want %d", got, binary, want)
			}

			if got := codecWidthOf(t, field.GetEncoding(), signed, 2); got != want {
				t.Errorf("codec reads PIC S9(2) COMP as %d bytes under %s, want %d", got, binary, want)
			}
		})
	}
}

// TestEveryStaircaseThisRepositoryResolvesUnderIsOneTheGeneratedCodeCanRead is
// the other half of #293's criterion: not merely that the two agree, but that
// there is no staircase a producer can reach where they cannot.
//
// Every member `copybook.Dialect.Validate` admits generates a package, and the
// Encoding that package declares names the codec member for it. An undeclared
// staircase is refused before it gets here — `copybook.NewLayout` rejects it
// with a DialectError naming the field — so the set below is the whole of what
// this repository can resolve under, and the generator has an answer for all of
// it. That is what makes the axis a fact carried across the IR rather than a
// constant this generator happens to share with the resolver.
func TestEveryStaircaseThisRepositoryResolvesUnderIsOneTheGeneratedCodeCanRead(t *testing.T) {
	t.Parallel()

	for binary, want := range map[copybook.BinarySize]string{
		copybook.BinarySize248:      "codec.BinarySize248",
		copybook.BinarySize1248:     "codec.BinarySize1248",
		copybook.BinarySizeSmallest: "codec.BinarySizeSmallest",
		copybook.BinarySizeFull:     "codec.BinarySizeFull",
	} {
		t.Run(binary.String(), func(t *testing.T) {
			t.Parallel()

			d := resolvedUnder(t, binary, staircaseCases["signed"], 4)

			out := t.TempDir()
			if err := generate(io.Discard, d, out, options{packageName: "bin", importPath: goldenModule + "internal/bin"}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			source := written(t, out)[codecFile]
			if !strings.Contains(source, "Binary:    "+want) {
				t.Errorf("the Encoding %s declares does not name %s:\n%s", codecFile, want, source)
			}
		})
	}
}

// TestAStaircaseNobodyDeclaredIsRefusedBeforeAnythingIsLaidOut is the other end
// of the same criterion: the staircases the generated code can read are all the
// ones this repository can resolve under, because resolving under one that names
// no staircase does not happen.
//
// The refusal is `cobol-go`'s and is raised by `copybook.NewLayout` before a
// single width is computed, which is why nothing downstream carries a guard for
// it. It is pinned here anyway, because "every staircase is readable" is only
// worth having beside "and there are no others" — and the diagnostic naming the
// dialect field is what tells an adopter which of the dialect's five members
// they left out rather than reporting a record that would not resolve.
func TestAStaircaseNobodyDeclaredIsRefusedBeforeAnythingIsLaidOut(t *testing.T) {
	t.Parallel()

	dialect := copybook.IBMEnterprise()
	dialect.Binary = copybook.BinarySizeUnset

	parsed, err := cobol.Parse(strings.NewReader(staircaseCopybook(staircaseCases["signed"], 4)), cobol.WithFragment())
	if err != nil {
		t.Fatalf("parsing the copybook: %v", err)
	}

	records, err := copybook.Build(parsed.Fragment.Entries)
	if err != nil {
		t.Fatalf("building the copybook: %v", err)
	}

	_, err = resolve.Resolve(records[0], resolve.Options{
		Copybook: "bin.cpy",
		Dialect:  dialect,
		Reading:  layoutmodel.ODOSlide,
		Encoding: layoutmodel.Axes{
			Charset:        layoutmodel.CP037,
			SignConvention: layoutmodel.SignEBCDIC,
			ByteOrder:      layoutmodel.BigEndian,
			FloatFormat:    layoutmodel.HFP,
		},
	})
	if err == nil {
		t.Fatal("a record resolved under a dialect naming no binary width staircase")
	}

	var refusal copybook.DialectError
	if !errors.As(err, &refusal) {
		t.Fatalf("resolving reported %v, want a copybook.DialectError", err)
	}

	if refusal.Field != "Binary" {
		t.Errorf("the refusal names the dialect's %s field, want Binary", refusal.Field)
	}

	if !strings.Contains(refusal.Error(), "BinarySize") {
		t.Errorf("the refusal reads %q and does not name the staircase type", refusal.Error())
	}
}

// TestAStaircaseThisBuildDoesNotKnowIsRefusedRatherThanGuessedAt is the arm
// neither [binarySize] nor [binaryValue] can be reached through today, and the
// reason both are written as total switches with an error arm.
//
// A descriptor naming a member past the last one this build knows was written
// against a newer IR. Reading it as the nearest member it does know would
// produce a package that compiles, round-trips against itself and puts every
// item behind the first COMP item at the wrong offset in an adopter's real
// file, which is the failure this axis exists to prevent rather than to
// relocate.
func TestAStaircaseThisBuildDoesNotKnowIsRefusedRatherThanGuessedAt(t *testing.T) {
	t.Parallel()

	ahead := irpb.BinarySize(int32(irpb.BinarySize_BINARY_SIZE_FULL) + 1)

	if _, err := binarySize(ahead); err == nil {
		t.Error("binarySize accepted a staircase past the last member this build knows")
	}

	if _, err := binaryValue(ahead); err == nil {
		t.Error("binaryValue accepted a staircase past the last member this build knows")
	}
}
