// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Zaba505/cobol-go/codec"

	"github.com/Zaba505/cpybkc/irpb"
)

// literalsFile is the file every byte string the package compares is declared
// in, beside what re-expresses them under another encoding.
//
// A file of its own because its contents belong to neither of the two files
// that compare: file.go compares a transition's predicate and a guard over a
// bytes register, codec.go compares an arm's predicate, and both compare
// against one set of literals re-expressed once. See docs/ir/SPEC.md, "A
// consumer may read under other axes, and re-expresses what it compares".
const literalsFile = "literals.go"

// The unexported identifiers literals.go declares, and the one a reader, a
// writer and a record method hold the literals in.
//
// Lowercase for the reason every other generated helper is: every identifier
// munged from a copybook name is exported, so none of these can collide with
// one.
const (
	literalsType     = "literals"
	resolvedLiterals = "resolvedLiterals"
	heldLiterals     = "heldLiterals"
	literalsFor      = "literalsFor"
	literalsUnder    = "literalsUnder"
	resolvedAxesFunc = "resolvedAxes"
	litsName         = "lits"
)

// emittedSources is the Go this generator copies into a package that compares
// a literal. See the doc comment on package emitted for why it is kept as Go
// rather than as strings here.
//
//go:embed emitted/*.go
var emittedSources embed.FS

// movement is how a literal crosses from the axes the descriptor resolved to
// the ones a reader or a writer was built with, which is decided by the item it
// is compared against and nothing else (docs/ir/SPEC.md, "Each axis carries a
// literal the way a file crosses it").
type movement int

const (
	// stays is an item no axis a caller may replace reaches: an item whose
	// charset is none, which carries bytes rather than characters (docs/ir/
	// SPEC.md, "An item with no charset carries bytes, not characters"), a
	// packed or COMP-6 item, and the INDEX, POINTER and NATIONAL items this
	// generator reads as bytes.
	stays movement = iota

	// characters is an item the charset reaches byte by byte: text, and a zoned
	// item whose every byte is a digit or a separate sign.
	characters

	// overpunched is a zoned item carrying an overpunched sign, whose sign byte
	// the sign convention reaches and whose other bytes the charset does.
	overpunched

	// reversed is a binary item, which the byte order reaches.
	reversed

	// valued is a floating-point item, which the float format reaches, and the
	// byte order too under IEEE 754.
	valued
)

// comparand is one byte string the generated package compares a field against.
type comparand struct {
	// name is its field in the generated literals struct.
	name string

	// value is its bytes as the descriptor resolved them.
	value []byte

	// field is the item it is compared against, and record the COBOL name of
	// the record holding that item. For a guard's literal the item is the
	// field the register was bound from. field is nil for a guard over a
	// register no binding fills from a field, which stays.
	field  *irpb.Node
	record string

	// moves is how it crosses to another encoding, and signAt is the byte
	// carrying an overpunched sign where moves is overpunched.
	moves  movement
	signAt int

	// used is whether anything emitted compares against it. Only those are
	// declared: a generated package is linted like any other, and a struct
	// field nothing reads is a finding.
	used bool
}

// overlapCheck is one pair of literals whose predicates `resolve` proved apart
// under the descriptor's axes and which re-expression could bring together, and
// where each starts in the bytes both predicates read.
type overlapCheck struct {
	one, other     *comparand
	oneAt, otherAt int
}

// literalTable is every literal of one descriptor, named once so that the
// three files naming them agree.
//
// It is gathered from the descriptor before anything is emitted, in the order
// the descriptor carries its nodes, so a literal's name is a function of the
// descriptor rather than of which file happened to reach it first. The emitters
// then mark what they compare and record the pairs a re-expression could bring
// together, and literals.go is composed last, out of what they marked.
type literalTable struct {
	// profile is the four axes the descriptor resolved, as the fields that
	// state them carry them; charset is unset where no field states one.
	profile *irpb.Encoding

	all    []*comparand
	byKey  map[string]*comparand
	source map[uint64]*irpb.Node

	checks  []overlapCheck
	checked map[string]bool

	// arms is whether an arm of a record's variant compares a literal. Such a
	// package's record methods compare one whatever its file-level reader and
	// writer do, so those are built by re-expressing the literals regardless:
	// a literal with no spelling under their encoding is refused when they are
	// built, not at the first record whose occurrence reaches it.
	arms bool
}

// gatherLiterals is every literal a descriptor's transitions, arms and guards
// compare.
//
// Only literals something references are gathered. A predicate or a guard no
// transition, state or arm names is compared by nothing, and re-expressing one
// would refuse an encoding over a literal no record is ever held to — the swap
// that fails for no reason the layout shows.
func gatherLiterals(d *irpb.Descriptor) (*literalTable, error) {
	e, err := newEmitter(d)
	if err != nil {
		return nil, err
	}

	t := &literalTable{
		byKey:   make(map[string]*comparand),
		source:  make(map[uint64]*irpb.Node),
		checked: make(map[string]bool),
	}

	if t.profile, err = literalProfile(d); err != nil {
		return nil, err
	}

	recordOf := make(map[uint64]string)
	arms := make(map[uint64]bool)
	predicates := make(map[uint64]bool)
	guards := make(map[uint64]bool)
	sources := make(map[uint64][]*irpb.Node)

	for _, node := range d.GetNodes() {
		switch kind := node.GetKind().(type) {
		case *irpb.Node_Record:
			e.fieldsOf(kind.Record.GetRootId(), kind.Record.GetNames().GetOriginal(), recordOf, arms, make(map[uint64]bool))
		case *irpb.Node_Transition:
			if kind.Transition.PredicateId != nil {
				predicates[kind.Transition.GetPredicateId()] = true
			}

			for _, id := range kind.Transition.GetGuardIds() {
				guards[id] = true
			}
		case *irpb.Node_State:
			if kind.State.GetAccepts() {
				for _, id := range kind.State.GetAcceptanceGuardIds() {
					guards[id] = true
				}
			}
		case *irpb.Node_Binding:
			if id, ok := kind.Binding.GetValue().(*irpb.Binding_FieldId); ok {
				if field, found := e.nodes[id.FieldId]; found && field.GetField() != nil {
					sources[kind.Binding.GetRegisterId()] = append(sources[kind.Binding.GetRegisterId()], field)
				}
			}
		}
	}

	for id := range arms {
		predicates[id] = true
	}

	for register, fields := range sources {
		for _, field := range fields[1:] {
			if movementOf(field.GetField()) != movementOf(fields[0].GetField()) || signAtOf(field.GetField()) != signAtOf(fields[0].GetField()) {
				return nil, &sharedRegisterError{Register: register, First: originalOf(fields[0]), Second: originalOf(field)}
			}
		}

		t.source[register] = fields[0]
	}

	for _, node := range d.GetNodes() {
		switch kind := node.GetKind().(type) {
		case *irpb.Node_Predicate:
			if !predicates[node.GetId()] {
				continue
			}

			field := e.nodes[kind.Predicate.GetFieldId()]
			if field.GetField() == nil {
				continue
			}

			if arms[node.GetId()] {
				t.arms = true
			}

			switch test := kind.Predicate.GetTest().(type) {
			case *irpb.Predicate_BytesEqual:
				t.add(test.BytesEqual.GetValue(), field, recordOf[field.GetId()])
			case *irpb.Predicate_BytesOneOf:
				for _, value := range test.BytesOneOf.GetValues() {
					t.add(value, field, recordOf[field.GetId()])
				}
			}
		case *irpb.Node_Guard:
			if !guards[node.GetId()] {
				continue
			}

			var values []*irpb.Literal

			switch test := kind.Guard.GetTest().(type) {
			case *irpb.Guard_Equals:
				values = []*irpb.Literal{test.Equals}
			case *irpb.Guard_OneOf:
				values = test.OneOf.GetValues()
			}

			field := t.source[kind.Guard.GetRegisterId()]

			for _, value := range values {
				if held, ok := value.GetValue().(*irpb.Literal_BytesValue); ok {
					t.add(held.BytesValue, field, recordOf[field.GetId()])
				}
			}
		}
	}

	return t, nil
}

// fieldsOf records, for every field the group id contains at any depth, the
// record it belongs to, and every predicate an arm it contains is selected by.
//
// The arms are gathered from here rather than off every variant node, because
// codec.go emits the variants the records contain and no others, and
// literals.go declares what codec.go compares.
//
// seen is what turns a descriptor whose containment has a cycle into a walk
// that ends; the emitters that walk the same nodes report the cycle.
func (e *emitter) fieldsOf(id uint64, record string, into map[uint64]string, arms, seen map[uint64]bool) {
	if seen[id] {
		return
	}

	seen[id] = true

	node, ok := e.nodes[id]
	if !ok {
		return
	}

	switch kind := node.GetKind().(type) {
	case *irpb.Node_Field:
		if _, held := into[id]; !held {
			into[id] = record
		}
	case *irpb.Node_Group:
		for _, member := range kind.Group.GetMemberIds() {
			e.fieldsOf(member, record, into, arms, seen)
		}
	case *irpb.Node_Variant:
		for _, a := range kind.Variant.GetArms() {
			if a.GetSchedule() == nil {
				arms[a.GetPredicateId()] = true
			}

			switch body := a.GetBody().(type) {
			case *irpb.Arm_GroupId:
				e.fieldsOf(body.GroupId, record, into, arms, seen)
			case *irpb.Arm_FieldId:
				e.fieldsOf(body.FieldId, record, into, arms, seen)
			}
		}
	}
}

// literalProfile is the four axes the descriptor's fields resolved, as
// [descriptorEncoding] reads them — and, where no field states a charset, the
// other three off the first field that carries them.
func literalProfile(d *irpb.Descriptor) (*irpb.Encoding, error) {
	enc, err := descriptorEncoding(d)
	if err != nil || enc != nil {
		return enc, err
	}

	for _, node := range d.GetNodes() {
		if field := node.GetField(); field != nil {
			return &irpb.Encoding{
				SignConvention: field.GetEncoding().GetSignConvention(),
				ByteOrder:      field.GetEncoding().GetByteOrder(),
				FloatFormat:    field.GetEncoding().GetFloatFormat(),
			}, nil
		}
	}

	return &irpb.Encoding{}, nil
}

// add gathers one literal, once per bytes and item.
func (t *literalTable) add(value []byte, field *irpb.Node, record string) {
	key := literalKey(value, field)
	if _, ok := t.byKey[key]; ok {
		return
	}

	one := &comparand{name: fmt.Sprintf("lit%d", len(t.all)+1), value: value, field: field, record: record, signAt: -1}

	if f := field.GetField(); f != nil {
		one.moves, one.signAt = movementOf(f), signAtOf(f)
	}

	t.all = append(t.all, one)
	t.byKey[key] = one
}

// literalKey is what makes two literals one: the same bytes, compared against
// the same item.
func literalKey(value []byte, field *irpb.Node) string {
	return strconv.FormatUint(field.GetId(), 10) + "\x00" + string(value)
}

// of is the literal value compared against field, marked as compared.
//
// A literal the gathering did not see is refused rather than answered: the
// gathering and the emitters walk the same nodes, and an answer here that was
// not the gathering's would be a generated package comparing against a field
// of the literals struct nothing declares.
func (t *literalTable) of(value []byte, field *irpb.Node) (*comparand, error) {
	one, ok := t.byKey[literalKey(value, field)]
	if !ok {
		return nil, malformed("a literal is compared that was not gathered before the package was emitted",
			"every predicate and guard a transition, a state or an arm names is gathered first, so that one set of literals serves every file comparing one")
	}

	one.used = true

	return one, nil
}

// ofGuard is the literal a guard over register compares against.
func (t *literalTable) ofGuard(value []byte, register uint64) (*comparand, error) {
	return t.of(value, t.source[register])
}

// collapses reports whether register's literals can come together under another
// encoding: whether the item it is bound from crosses an axis by a mapping that
// may send two bytes to one. A guard over such a register cannot be relied on
// to keep two transitions apart once the literals are re-expressed.
func (t *literalTable) collapses(register uint64) bool {
	field := t.source[register]
	if field == nil {
		return false
	}

	switch movementOf(field.GetField()) {
	case stays, characters:
		return false
	default:
		return true
	}
}

// overlap records that one and other, starting at oneAt and otherAt in the
// bytes both of their predicates read, are two literals `resolve` held apart
// and a reader or writer built under another encoding has to hold apart again.
//
// A pair re-expression cannot bring together is not recorded. Two literals that
// never move stay as they were, and two moved byte by byte through one code
// page stay distinct because a code page is a bijection; two binary literals of
// one width at one offset are reversed alike. Every other pair — a sign byte
// beside a character, a float, two items moved by different axes over one run —
// could come to one byte string, and is checked once the encoding is known.
func (t *literalTable) overlap(one *comparand, oneAt int, other *comparand, otherAt int) {
	if one == other {
		return
	}

	if one.moves == other.moves && (one.moves == stays || one.moves == characters) {
		return
	}

	if one.moves == reversed && other.moves == reversed && oneAt == otherAt && len(one.value) == len(other.value) {
		return
	}

	if one.name > other.name || (one.name == other.name && oneAt > otherAt) {
		one, other, oneAt, otherAt = other, one, otherAt, oneAt
	}

	key := fmt.Sprintf("%s@%d/%s@%d", one.name, oneAt, other.name, otherAt)
	if t.checked[key] {
		return
	}

	t.checked[key] = true
	t.checks = append(t.checks, overlapCheck{one: one, other: other, oneAt: oneAt, otherAt: otherAt})
}

// compared reports whether anything emitted compares a literal, which is
// whether literals.go is written at all.
func (t *literalTable) compared() bool {
	for _, one := range t.all {
		if one.used {
			return true
		}
	}

	return false
}

// movementOf is how a literal compared against f crosses to another encoding.
func movementOf(f *irpb.Field) movement {
	switch f.GetUsage() {
	case irpb.Usage_USAGE_DISPLAY:
		if f.GetEncoding().GetCharset() == irpb.Charset_CHARSET_NONE {
			return stays
		}

		if f.GetPicture().GetCategory() != irpb.Category_CATEGORY_NUMERIC || signAtOf(f) < 0 {
			return characters
		}

		return overpunched
	case irpb.Usage_USAGE_BINARY, irpb.Usage_USAGE_COMP_5:
		return reversed
	case irpb.Usage_USAGE_COMP_1, irpb.Usage_USAGE_COMP_2:
		return valued
	default:
		return stays
	}
}

// signAtOf is the byte of a zoned item carrying an overpunched sign, or -1
// where it carries none — an item that is not zoned, an unsigned one, and one
// whose sign is SEPARATE, which is a character like the digits beside it.
func signAtOf(f *irpb.Field) int {
	if f.GetUsage() != irpb.Usage_USAGE_DISPLAY || f.GetPicture().GetCategory() != irpb.Category_CATEGORY_NUMERIC {
		return -1
	}

	switch f.GetPicture().GetSignPosition() {
	case irpb.SignPosition_SIGN_POSITION_LEADING:
		return 0
	case irpb.SignPosition_SIGN_POSITION_TRAILING:
		return int(f.GetWidth()) - 1
	default:
		return -1
	}
}

// described is a literal as a refusal and a doc comment name it: its bytes,
// and the characters they spell where they are characters.
func (t *literalTable) described(one *comparand) string {
	text := "the literal " + strconv.Quote(string(one.value))

	if one.moves != characters {
		return text
	}

	var charset codec.Charset

	switch t.profile.GetCharset() {
	case irpb.Charset_CHARSET_CP037:
		charset = codec.CP037()
	case irpb.Charset_CHARSET_ASCII:
		charset = codec.ASCII()
	default:
		return text
	}

	runes := make([]rune, 0, len(one.value))
	for _, b := range one.value {
		runes = append(runes, charset.ToUnicode(b))
	}

	return text + ", " + strconv.Quote(string(runes)) + " under " + charsetName(t.profile.GetCharset())
}

// itemOf is the COBOL name of the item a literal is compared against, for a
// refusal.
func itemOf(one *comparand) string {
	if one.field == nil {
		return "a register no binding fills from an item"
	}

	if name := originalOf(one.field); name != "" {
		return name
	}

	return "an item the copybook gives no data-name"
}

// reflowed is text whose paragraphs are each [wrapped], for a doc comment
// composed around identifiers whose length the text does not know.
func reflowed(text string) string {
	paragraphs := strings.Split(text, "\n\n")
	for i, paragraph := range paragraphs {
		paragraphs[i] = wrapped(paragraph)
	}

	return strings.Join(paragraphs, "\n\n")
}

// literalsSource is the source of [literalsFile], or the empty string where the
// package compares no literal.
func literalsSource(t *literalTable, opts options) (string, error) {
	if !t.compared() {
		return "", nil
	}

	var used []*comparand

	for _, one := range t.all {
		if one.used {
			used = append(used, one)
		}
	}

	has := func(moves ...movement) bool {
		for _, one := range used {
			if slices.Contains(moves, one.moves) {
				return true
			}
		}

		return false
	}

	// The axes that reach an item some literal here is compared against, as
	// the test that each is the descriptor's own. Only these decide whether a
	// reader or a writer compares the literals as resolved: an axis no
	// literal's item depends on moves none of them.
	var governs, named []string

	if has(characters, overpunched) {
		governs = append(governs, "sameCharset(enc.Charset, from.Charset)")
		named = append(named, "charset")
	}

	if has(overpunched) {
		governs = append(governs, "enc.Sign == from.Sign")
		named = append(named, "sign convention")
	}

	if has(reversed, valued) {
		governs = append(governs, "sameByteOrder(enc.ByteOrder, from.ByteOrder)")
		named = append(named, "byte order")
	}

	if has(valued) {
		governs = append(governs, "enc.Float == from.Float")
		named = append(named, "float format")
	}

	if len(governs) == 0 {
		return steadySource(t, opts, used), nil
	}

	files := []string{"held.go"}

	if has(characters, overpunched, valued) {
		files = append(files, "refusal.go")
	}

	if has(characters, overpunched) {
		files = append(files, "text.go")
	}

	if has(overpunched) {
		files = append(files, "zoned.go")
	}

	if has(reversed) {
		files = append(files, "binary.go")
	}

	if has(valued) {
		files = append(files, "float.go")
	}

	if len(t.checks) != 0 {
		files = append(files, "overlap.go")
	}

	imports := map[string]bool{codecImport: true, "encoding/binary": true, "sync/atomic": true}

	var bodies []string

	for _, name := range files {
		paths, body, err := emittedFile(name)
		if err != nil {
			return "", err
		}

		for _, path := range paths {
			imports[path] = true
		}

		bodies = append(bodies, body)
	}

	axes, err := resolvedAxesSource(t.profile)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	line(&b, "%s", generatedBy)
	line(&b, "")
	line(&b, "package %s", opts.packageName)
	line(&b, "")
	line(&b, "import (")

	paths := make([]string, 0, len(imports))
	for path := range imports {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	for _, path := range paths {
		line(&b, "%q", path)
	}

	line(&b, ")")
	line(&b, "")

	t.emitDeclared(&b, used)
	b.WriteString(axes)
	line(&b, "")
	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%s is every re-expression this package has made, by the encoding it
was made under. See reexpressedUnder.`, heldLiterals))))
	line(&b, "var %s atomic.Pointer[[]reexpression[%s]]", heldLiterals, literalsType)
	line(&b, "")
	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%[1]s is the literals as a file under enc spells them.

Where enc is the descriptor's own on every axis an item compared here depends
on — the %[4]s — that is %[2]s, and nothing is re-expressed: the comparisons a
reader and a writer make are the ones the descriptor resolved. Otherwise it is
%[3]s's answer, worked out the first time this package meets enc and held
after that — so the record methods, which are handed an encoding on every
call, re-express once per encoding rather than once per record. Asking costs
the comparisons that decide whether enc moves anything and, where it does,
one lookup.

A literal that has no spelling under enc is refused here, which is before
any record is read or written under it.`, literalsFor, resolvedLiterals, literalsUnder, joinNames(named)))))
	line(&b, "func %s(enc codec.Encoding) (*%s, error) {", literalsFor, literalsType)
	line(&b, "from := %s(enc)", resolvedAxesFunc)
	line(&b, "")
	line(&b, "if %s {", strings.Join(governs, " && "))
	line(&b, "return &%s, nil", resolvedLiterals)
	line(&b, "}")
	line(&b, "")
	line(&b, "return reexpressedUnder(&%s, enc, %s)", heldLiterals, literalsUnder)
	line(&b, "}")
	line(&b, "")
	t.emitUnder(&b, used)

	for _, body := range bodies {
		b.WriteString(body)
	}

	return b.String(), nil
}

// emitDeclared writes the literals struct and the literals as the descriptor
// resolved them.
func (t *literalTable) emitDeclared(b *strings.Builder, used []*comparand) {
	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%[1]s is every byte string this package compares a field against: the
literals of a transition's predicate, of an arm's predicate and of a guard over
a bytes register.

Each is the bytes a file under the descriptor's own axes holds there, which is
what %[2]s holds them as. A reader or a writer built under other axes — a
converted copy of the same file, say — compares against them as a file under
those axes spells them instead, re-expressed once, before its first record,
by %[3]s. See docs/ir/SPEC.md, "A consumer may read under other axes, and
re-expresses what it compares".`, literalsType, resolvedLiterals, literalsFor))))
	line(b, "type %s struct {", literalsType)

	for i, one := range used {
		if i > 0 {
			line(b, "")
		}

		b.WriteString(commentLines(wrapped(fmt.Sprintf("%s is what %s of %s is compared against: %s.", one.name, itemOf(one), one.record, t.described(one)))))
		line(b, "%s []byte", one.name)
	}

	line(b, "}")
	line(b, "")
	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%s is every literal as the descriptor resolved it. A reader or a writer
under the descriptor's own axes compares against these and nothing else.`, resolvedLiterals))))
	line(b, "var %s = %s{", resolvedLiterals, literalsType)

	for _, one := range used {
		line(b, "%s: []byte(%s),", one.name, strconv.Quote(string(one.value)))
	}

	line(b, "}")
	line(b, "")
}

// steadySource is literals.go for a package none of whose literals any axis a
// caller may replace reaches: every one is compared against an item whose
// charset is none, a packed item, or another whose bytes no axis moves. There
// is nothing to re-express, so nothing to hold and nothing to refuse, and the
// literals under any encoding are the ones the descriptor resolved.
func steadySource(t *literalTable, opts options, used []*comparand) string {
	var b strings.Builder

	line(&b, "%s", generatedBy)
	line(&b, "")
	line(&b, "package %s", opts.packageName)
	line(&b, "")
	line(&b, "import %q", codecImport)
	line(&b, "")
	t.emitDeclared(&b, used)
	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%s is the literals as a file under enc spells them, which is %s
under every encoding: no literal here is compared against an item any of the
four axes a caller may replace reaches, so none of them moves.`, literalsFor, resolvedLiterals))))
	line(&b, "func %s(codec.Encoding) (*%s, error) {", literalsFor, literalsType)
	line(&b, "return &%s, nil", resolvedLiterals)
	line(&b, "}")

	return b.String()
}

// emitUnder writes the function re-expressing every literal under one
// encoding, and holding the pairs that could have come together apart again.
func (t *literalTable) emitUnder(b *strings.Builder, used []*comparand) {
	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%s is every literal re-expressed under enc, each through the axes that
reach the item it is compared against and no other: a literal whose item enc
does not move is carried as it is, and never refused on its own account.`, literalsUnder))))
	line(b, "func %s(enc codec.Encoding) (*%s, error) {", literalsUnder, literalsType)

	moving, fallible := len(t.checks) != 0, false

	for _, one := range used {
		switch one.moves {
		case characters, overpunched, valued:
			moving, fallible = true, true
		case reversed:
			moving = true
		}
	}

	if moving {
		line(b, "from := %s(enc)", resolvedAxesFunc)
	}

	line(b, "l := %s", resolvedLiterals)

	if fallible {
		line(b, "")
		line(b, "var err error")
	}

	for _, one := range used {
		refusal := fmt.Sprintf("return nil, cannotReexpress(%s, %s, %s, err)",
			strconv.Quote(t.described(one)), strconv.Quote(itemOf(one)), strconv.Quote(one.record))

		switch one.moves {
		case characters:
			line(b, "")
			line(b, "if l.%[1]s, err = reexpressText(l.%[1]s, from.Charset, enc.Charset); err != nil {", one.name)
			line(b, "%s", refusal)
			line(b, "}")
		case overpunched:
			line(b, "")
			line(b, "if l.%[1]s, err = reexpressZoned(l.%[1]s, from, enc, %[2]d); err != nil {", one.name, one.signAt)
			line(b, "%s", refusal)
			line(b, "}")
		case valued:
			line(b, "")
			line(b, "if l.%[1]s, err = reexpressFloat(l.%[1]s, from, enc); err != nil {", one.name)
			line(b, "%s", refusal)
			line(b, "}")
		case reversed:
			line(b, "")
			line(b, "l.%[1]s = reexpressBinary(l.%[1]s, from.ByteOrder, enc.ByteOrder)", one.name)
		}
	}

	if len(t.checks) != 0 {
		line(b, "")
		line(b, "// Pairs `resolve` proved apart under the descriptor's axes, whose proof a")
		line(b, "// re-expression does not carry: held to it again, under enc.")
	}

	for _, check := range t.checks {
		line(b, "if !literalsApart(l.%s, %d, l.%s, %d) {", check.one.name, check.oneAt, check.other.name, check.otherAt)
		line(b, "return nil, literalsNotApart(from, enc, %s, %s, %s, %s, %s, %s)",
			strconv.Quote(t.described(check.one)), strconv.Quote(itemOf(check.one)), strconv.Quote(check.one.record),
			strconv.Quote(t.described(check.other)), strconv.Quote(itemOf(check.other)), strconv.Quote(check.other.record))
		line(b, "}")
	}

	line(b, "")
	line(b, "return &l, nil")
	line(b, "}")
}

// resolvedAxesSource is the function putting the descriptor's four axes back
// on an encoding.
//
// A function of the caller's encoding rather than a value, because the
// staircase is not one of the four and is left as the caller's: comparing it is
// not what deciding whether a literal moves is about.
func resolvedAxesSource(profile *irpb.Encoding) (string, error) {
	var b strings.Builder

	b.WriteString(commentLines(reflowed(fmt.Sprintf(`%s is enc with the four axes a layout states — charset, sign convention,
byte order and float format — put back to the ones this descriptor resolved,
which is the encoding every literal in %s is spelled under.`, resolvedAxesFunc, resolvedLiterals))))
	line(&b, "func %s(enc codec.Encoding) codec.Encoding {", resolvedAxesFunc)

	if profile.GetCharset() != irpb.Charset_CHARSET_UNSPECIFIED && profile.GetCharset() != irpb.Charset_CHARSET_NONE {
		charset, err := charsetCall(profile.GetCharset())
		if err != nil {
			return "", err
		}

		line(&b, "enc.Charset = %s", charset)
	}

	line(&b, "enc.Sign = %s", signConvention(profile.GetSignConvention()))
	line(&b, "enc.ByteOrder = %s", byteOrder(profile.GetByteOrder()))
	line(&b, "enc.Float = %s", floatFormat(profile.GetFloatFormat()))
	line(&b, "")
	line(&b, "return enc")
	line(&b, "}")

	return b.String(), nil
}

// emittedFile is one file of package emitted as literals.go takes it: the
// paths it imports, and everything below its import block.
func emittedFile(name string) ([]string, string, error) {
	src, err := emittedSources.ReadFile("emitted/" + name)
	if err != nil {
		return nil, "", fmt.Errorf("%s carries no emitted/%s, which is a bug in %s: %w", pluginName, name, pluginName, err)
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
	if err != nil {
		return nil, "", fmt.Errorf("emitted/%s does not parse, which is a bug in %s: %w", name, pluginName, err)
	}

	paths := make([]string, 0, len(file.Imports))

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, "", fmt.Errorf("emitted/%s imports %s, which is a bug in %s: %w", name, spec.Path.Value, pluginName, err)
		}

		paths = append(paths, path)
	}

	end := file.Name.End()

	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			end = gen.End()
		}
	}

	return paths, string(src[fset.Position(end).Offset:]), nil
}
