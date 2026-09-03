// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// rivalsOf is the transitions of one state whose predicates a writer taking
// walk[at] has to evaluate against the bytes it is about to emit, by position in
// the state's transition list.
//
// The reader stops at the first eligible transition whose predicate matches, and
// the writer narrows to the transitions admitting the record it was handed —
// which is not the reader's algorithm and cannot be, since a predicate belonging
// to a transition admitting some other record names a field at an offset the
// record in hand may not even reach. What used to make the narrowed walk land
// where the reader lands was the overlap rule: where no two transitions leaving
// a state can both match one input, bytes satisfying this record's predicate
// satisfy no earlier one's. docs/ir/SPEC.md's "A batch boundary is told by the
// order" gives that up for one shape, so the derivation is replaced here rather
// than dropped — the writer spends the order on the writing side too, and
// refuses a record an earlier transition would have taken (#332, #333).
//
// Three of the four transitions leaving a state are not rivals, and each is
// skipped for a reason rather than for economy. Emitting a test that can never
// fire is dead code in every generated package that has one, and the goldens say
// which those are.
//
// **A transition ordered after this one is not a rival.** The reader reaches it
// only where everything ahead of it failed, and this record's own transition is
// ahead of it, so it never sees the record at all.
//
// **A transition carrying no predicate is not a rival.** It matches every
// record, so a state offering one beside an eligible sibling is a state
// docs/ir/SPEC.md's "A transition may carry no predicate" refuses outright: the
// pair never reaches a generator. Testing one anyway would refuse every record
// this state admits.
//
// **A transition whose run shares a byte with this one's is not a rival.** A
// pair whose runs meet is told apart by the literals over the bytes they share,
// and a pair agreeing there is an ambiguity `resolve` refuses. So a descriptor
// carrying such a pair carries literals that disagree somewhere in the shared
// window, and bytes satisfying this record's predicate cannot satisfy the
// other's. This is the case every golden in this repository is in, which is why
// none of them changed when this landed.
//
// **A transition whose guards cannot hold at the same time as this one's is not
// a rival.** It is never eligible where this one is, so the reader never
// evaluates its predicate at all. That is the counted run — the transition
// reading another detail and the one moving past them are selected by the very
// same test on the very same bytes, and only the counter separates them.
//
// **A transition admitting the record being written is not a rival either.** The
// writer's narrowed walk is every transition of this state admitting this record
// type, in the order the state carries them, and each takes the record and
// returns where its guards hold and its predicate matches. So reaching the one
// at `at` is already proof that every earlier one of them did neither, which is
// the same answer a test here would compute a second time.
//
// What is left is a pair reading two runs that never meet, both eligible at
// once: the pair the order resolves, and the only pair a writer can forge a
// boundary with.
//
// It is deliberately one step coarser than [overlap] in `internal/resolve`,
// which also consults the two copybooks' byte domains and can prove a pair
// exclusive whose runs never meet (#330). That proof is a reading of a PICTURE
// and a USAGE, it lives where the copybooks are, and the IR carries no record of
// it having been taken — so re-deriving it here would be a second reading of an
// encoding kept in a second place, which is the disagreement docs/ir/SPEC.md
// declines elsewhere. The cost of being coarser is a test that cannot fire on a
// pair the copybooks already separate; the cost of guessing the other way would
// be a file this format's own reader routes elsewhere.
func (f *filer) rivalsOf(walk []transition, at int) []int {
	taken := walk[at]

	if taken.match == "" {
		return nil
	}

	var out []int

	for i := range at {
		other := walk[i]

		if other.match == "" || other.typ == taken.typ || shareBytes(taken, other) || !f.coEligible(taken, other) {
			continue
		}

		out = append(out, i)
	}

	return out
}

// shareBytes reports whether two transitions' predicates read runs covering one
// byte of the record.
//
// Both runs are half-open — `at` is the first byte and `reads` is one past the
// last, which is what [filer.predicate] returns — so they meet exactly where
// each begins before the other ends.
func shareBytes(one, other transition) bool {
	return one.at < other.reads && other.at < one.reads
}

// coEligible reports whether some register file makes both transitions eligible
// at once.
//
// The same question `internal/resolve`'s `satisfiable` asks, asked here of the
// IR's guard nodes rather than of the compiler's, and asked of the two guard
// lists concatenated: a guard list is a conjunction, so two transitions are
// co-eligible exactly where the concatenation of their lists is satisfiable.
//
// It stays a question about literals and zero because of what a guard is not: a
// flat conjunction over a fixed set of declared registers, with no arithmetic in
// it beyond taking one off a counter and no comparison of one register against
// another (docs/ir/SPEC.md, "The automaton counts; it does not compute").
//
// Every answer this cannot reach is `true`, and the asymmetry is the whole of
// what makes it safe: a pair wrongly called co-eligible costs a test that never
// fires, and a pair wrongly called exclusive costs the check this file exists to
// emit. A malformed descriptor is therefore not diagnosed here — the emitters
// that read these same nodes for their tests already report one, and a second
// diagnostic from a helper deciding whether to emit a test would say the same
// thing twice.
func (f *filer) coEligible(one, other transition) bool {
	byRegister := make(map[uint64][]*irpb.Guard)

	for _, id := range slices.Concat(one.node.GetGuardIds(), other.node.GetGuardIds()) {
		node, ok := f.nodes[id]
		if !ok {
			return true
		}

		guard := node.GetGuard()
		if guard == nil {
			return true
		}

		byRegister[guard.GetRegisterId()] = append(byRegister[guard.GetRegisterId()], guard)
	}

	for _, over := range byRegister {
		if !holdsTogether(over) {
			return false
		}
	}

	return true
}

// holdsTogether reports whether every guard over one register can hold at once.
//
// The values the register may hold are narrowed test by test: `equals` and
// `one-of` narrow to a set of literals, and `positive` drops the ones that are
// not numbers above zero. A literal carrying bytes survives `positive`, because
// a guard over a bytes register never carries one beside an `equals` — the two
// are over registers of different kinds — and calling such a pair unsatisfiable
// would suppress a check on the strength of a contradiction that is not there.
func holdsTogether(guards []*irpb.Guard) bool {
	var narrowed []string

	bounded, positive := false, false

	for _, guard := range guards {
		switch test := guard.GetTest().(type) {
		case *irpb.Guard_GreaterThanZero:
			positive = true
		case *irpb.Guard_Equals:
			ids, ok := identities([]*irpb.Literal{test.Equals})
			if !ok {
				return true
			}

			narrowed, bounded = narrow(narrowed, ids, bounded)
		case *irpb.Guard_OneOf:
			ids, ok := identities(test.OneOf.GetValues())
			if !ok {
				return true
			}

			narrowed, bounded = narrow(narrowed, ids, bounded)
		default:
			return true
		}
	}

	if !positive {
		return !bounded || len(narrowed) > 0
	}

	if !bounded {
		return true
	}

	return slices.ContainsFunc(narrowed, aboveZero)
}

// narrow is the values still admitted after one more test over the register.
func narrow(held, values []string, bounded bool) ([]string, bool) {
	if !bounded {
		return values, true
	}

	return slices.DeleteFunc(slices.Clone(held), func(one string) bool {
		return !slices.Contains(values, one)
	}), true
}

// identities is one test's literals as the strings two literals are the same
// value exactly when they share, and whether every one of them could be read.
//
// The kind is written into the identity rather than left to the bytes, so that
// an integer register holding 48 and a bytes register holding "0" are two values
// however the charset spells the second.
func identities(values []*irpb.Literal) ([]string, bool) {
	out := make([]string, 0, len(values))

	for _, value := range values {
		switch held := value.GetValue().(type) {
		case *irpb.Literal_Integer:
			out = append(out, "integer:"+strconv.FormatInt(held.Integer, 10))
		case *irpb.Literal_BytesValue:
			out = append(out, "bytes:"+string(held.BytesValue))
		default:
			return nil, false
		}
	}

	return out, true
}

// aboveZero reports whether an identity is one a `positive` guard admits.
func aboveZero(id string) bool {
	number, ok := strings.CutPrefix(id, "integer:")
	if !ok {
		return true
	}

	n, err := strconv.ParseInt(number, 10, 64)

	return err != nil || n > 0
}

// emitRivalChecks writes the evaluation of every earlier transition's predicate
// against the bytes the writer is about to emit, and the refusal of a record one
// of them matches.
//
// It stands between the predicate that selected this transition and the emit
// that would have written the record out, because that is the last point at
// which the bytes exist and nothing has gone to the io.Writer yet. Refusing here
// costs one diagnostic; emitting costs a file whose own reader reads this record
// as some other record type, and a batch boundary nothing downstream can tell
// from a real one.
//
// checked is the registers this branch has already found bound, which is the
// taken transition's own guards' — a second `if !w.registerNBound` under the
// first would be unreachable and would read as though the answer could have
// changed in between.
func (f *filer) emitRivalChecks(b *strings.Builder, walk []transition, at int, checked []uint64) error {
	taken := walk[at]
	record := taken.record.GetNames().GetOriginal()

	for _, i := range f.rivalsOf(walk, at) {
		other := walk[i]

		f.forges = true

		item, err := f.targetOf(other)
		if err != nil {
			return err
		}

		matches, err := f.matcherOf(other)
		if err != nil {
			return err
		}

		test, _, registers, err := f.guardTests(other, "w")
		if err != nil {
			return err
		}

		for _, id := range registers {
			if slices.Contains(checked, id) {
				continue
			}

			line(b, "if !w.%s {", held(id))
			line(b, "return w.unbound(%d)", id)
			line(b, "}")
			line(b, "")

			checked = append(checked, id)
		}

		line(b, "// Transition %d of that state is evaluated before this one and reads bytes", i+1)
		line(b, "// %d:%d, which is not the run the predicate above tested. A reader follows the", other.at, other.reads)
		line(b, "// same order, so a record matching it here is a record this file's own reader")
		line(b, "// admits as %s. See docs/ir/SPEC.md, \"A writer walks the same", other.record.GetNames().GetOriginal())
		line(b, "// automaton\".")

		if test == "" {
			line(b, "if %s(raw) {", matches)
		} else {
			line(b, "if %s && %s(raw) {", test, matches)
		}

		line(b, "return w.refuse(%q, fmt.Sprintf(%q, raw[%d:%d]))",
			escaped(record), forged(escaped(item), escaped(other.record.GetNames().GetOriginal()), other.at, other.reads),
			other.at, other.reads)
		line(b, "}")
		line(b, "")
	}

	return nil
}

// forged is what a writer says about a record an earlier transition would have
// taken, as the reason [filer.emitWriterDiagnostics]'s refusal carries.
//
// A format string, and the one `%q` in it is why its two names arrive escaped
// rather than being escaped here: the bytes that did it are read out of the
// record at run time, so the sentence around them is a format and a name
// carrying a per cent sign would take the place of them.
//
// Four things, because a reader of this message has to be able to act on it
// without the descriptor in front of them: which bytes of the record did it,
// what they hold, the item whose value they would be read as, and the record
// type this record would come back as. The bytes themselves are the caller's to
// change and every other part of the sentence is what says which ones.
func forged(item, record string, at, reads int) string {
	return "bytes " + strconv.Itoa(at) + ":" + strconv.Itoa(reads) + " of it hold %q, which is the value " +
		item + " carries in a " + record + ". The transition admitting that record leaves this state ahead of the one " +
		"this record would have taken, and a reader takes the first that matches, so this record would be read back as a " +
		record + " — the two are told apart by the order alone and by nothing in the bytes"
}

// targetOf is the copybook's name for the item one transition's predicate reads.
func (f *filer) targetOf(t transition) (string, error) {
	node, ok := f.nodes[t.node.GetPredicateId()]
	if !ok {
		return "", unresolved(t.node.GetPredicateId())
	}

	target, ok := f.nodes[node.GetPredicate().GetFieldId()]
	if !ok {
		return "", unresolved(node.GetPredicate().GetFieldId())
	}

	return originalOf(target), nil
}
