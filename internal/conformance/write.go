// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// WriteValue is what one node of a descriptor holds, written in the value
// language: an object for a group, an array where the node repeats, and a
// string for an elementary item, in the spelling
// docs/conformance/SPEC.md's "The value language" states.
//
// The value is a [reflect.Value] over whatever the generated code decoded the
// record into, walked beside the descriptor rather than named field by field,
// which is what lets one writer serve every entry. What that requires of the
// decoded value is only what the Go generator's own record types provide: a
// group is a struct whose exported fields stand for its members in order, a
// repeating node is an array or a slice, a variant's arms are one pointer each
// of which exactly one is non-nil, and an elementary item is a string, an
// integer, a float or a run of bytes.
//
// It lives here, beside the reader of the same language, rather than in the
// codec program [github.com/Zaba505/cpybkc/internal/conformance/goadapter] writes,
// for the reason docs/conformance/GRAMMAR.md exists at all: a values document that
// is written wrongly presents as a generator that decoded wrongly, which is the
// most expensive way there is to learn about a formatting mistake. Code inside
// a template is checked by compiling a scratch program per corpus entry and by
// nothing else, so the one part of it that is about the *format* rather than
// about the run is out here, where go vet, the linter and a table of values
// against their exact text can all reach it.
func WriteValue(nodes map[uint64]*irpb.Node, node *irpb.Node, value reflect.Value) (any, error) {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Group:
		if kind.Group.GetRepetition() != nil {
			return writeOccurrences(value, func(one reflect.Value) (any, error) {
				return writeGroup(nodes, kind.Group, one)
			})
		}

		return writeGroup(nodes, kind.Group, value)
	case *irpb.Node_Field:
		if kind.Field.GetRepetition() != nil {
			return writeOccurrences(value, func(one reflect.Value) (any, error) {
				return WriteScalar(one)
			})
		}

		return WriteScalar(value)
	default:
		return nil, fmt.Errorf("node %d is not an item of a record", node.GetId())
	}
}

// writeGroup is what a group node holds: one key per member, named as the
// copybook names it.
//
// The members are walked beside the struct's exported fields in order, which is
// the order the generator emits them in. A slack node takes no field — its bytes
// are held unexported and are not a value anybody decoded — and a variant takes
// one field per arm, of which the occurrence holds exactly one.
func writeGroup(nodes map[uint64]*irpb.Node, node *irpb.Group, value reflect.Value) (any, error) {
	if value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s decoded into a %s and not a struct", node.GetNames().GetOriginal(), value.Kind())
	}

	fields := exportedFields(value)
	held := map[string]any{}

	taken := 0

	take := func() (reflect.Value, error) {
		if taken >= len(fields) {
			return reflect.Value{}, fmt.Errorf("%s carries fewer fields than the descriptor describes members",
				node.GetNames().GetOriginal())
		}

		field := fields[taken]
		taken++

		return field, nil
	}

	for _, id := range node.GetMemberIds() {
		member, ok := nodes[id]
		if !ok {
			return nil, fmt.Errorf("member %d of %s is not a node of this descriptor", id, node.GetNames().GetOriginal())
		}

		if member.GetSlack() != nil {
			continue
		}

		if variant := member.GetVariant(); variant != nil {
			if err := writeArms(nodes, variant, take, held); err != nil {
				return nil, err
			}

			continue
		}

		field, err := take()
		if err != nil {
			return nil, err
		}

		decoded, err := WriteValue(nodes, member, field)
		if err != nil {
			return nil, err
		}

		held[original(member)] = decoded
	}

	if taken != len(fields) {
		return nil, fmt.Errorf("%s carries %d fields and the descriptor describes %d members",
			node.GetNames().GetOriginal(), len(fields), taken)
	}

	return held, nil
}

// writeArms reads the one alternative an occurrence holds.
//
// Every arm takes a field, whether or not the occurrence took that alternative,
// because the struct carries one pointer per arm and exactly one of them is
// non-nil. An arm the occurrence did not take contributes no key at all rather
// than a null, which is what says that the item is not there.
func writeArms(nodes map[uint64]*irpb.Node, variant *irpb.Variant, take func() (reflect.Value, error), held map[string]any) error {
	for _, arm := range variant.GetArms() {
		field, err := take()
		if err != nil {
			return err
		}

		var id uint64

		switch body := arm.GetBody().(type) {
		case *irpb.Arm_GroupId:
			id = body.GroupId
		case *irpb.Arm_FieldId:
			id = body.FieldId
		default:
			return fmt.Errorf("an arm of a variant names no body")
		}

		body, ok := nodes[id]
		if !ok {
			return fmt.Errorf("the body %d of an arm is not a node of this descriptor", id)
		}

		if field.Kind() != reflect.Pointer {
			return fmt.Errorf("%s decoded into a %s and not a pointer", nodeName(body), field.Kind())
		}

		if field.IsNil() {
			continue
		}

		decoded, err := WriteValue(nodes, body, field.Elem())
		if err != nil {
			return err
		}

		held[original(body)] = decoded
	}

	return nil
}

// writeOccurrences is what a repeating node holds: one value per occurrence, in
// order.
func writeOccurrences(value reflect.Value, one func(reflect.Value) (any, error)) (any, error) {
	if kind := value.Kind(); kind != reflect.Array && kind != reflect.Slice {
		return nil, fmt.Errorf("an item that repeats decoded into a %s and not an array or a slice", kind)
	}

	held := make([]any, 0, value.Len())

	for i := range value.Len() {
		decoded, err := one(value.Index(i))
		if err != nil {
			return nil, err
		}

		held = append(held, decoded)
	}

	return held, nil
}

// WriteScalar is what an elementary item holds, written the way the value
// language writes it: characters as text with every trailing space removed, a
// number as its decimal digits, a float in hexadecimal significand notation,
// and a run of bytes as base64.
//
// Every one of those is a JSON string. A float in particular is never a JSON
// number: NaN and the infinities cannot be marshalled as one, so an honest
// generator decoding an IEEE NaN would make a driver fail to write a document
// at all — which the corpus format defines as the harness breaking, the one
// outcome that is not a conformance failure.
//
// Which of the four an item takes is a function of its usage and category, and
// it is not read here: the generated code has already applied it, in the Go
// type it gave the field. docs/conformance/SPEC.md's "Which form a value takes
// is decided by the descriptor" is the mapping, and cmd/cpybkc-gen-go's
// README.md is the Go type each row of it lands on.
//
// The trailing spaces are removed here rather than relied on to have been
// removed upstream. The rule is the format's requirement on *a writer of a
// values document*, and this is one; a decoder that trims is a decoder that
// happens to agree with it, and a value language whose rule is enforced only by
// a dependency is a rule this repository cannot be held to. Trimming an already
// trimmed value costs nothing and changes no answer the corpus holds today.
//
// It does change what a corpus run *measures*, and in the direction of
// measuring less: a generator whose accessor left the padding on an alphanumeric
// item used to fail every entry that carries one, and now has it trimmed away
// here instead. That is deliberate. What the corpus compares is two readings of
// one descriptor written in one language, and the padding is not part of either
// — docs/conformance/SPEC.md says at length that it is a width the value
// language does not carry, so an entry that failed on it was reporting a
// disagreement about the *document* rather than about the record. What an
// accessor returns is cmd/cpybkc-gen-go's business and its own tests', and a
// corpus entry is the wrong instrument for it: no entry could say which of the
// two ends of the pipeline had kept the spaces.
//
// The return is a string rather than an any because every one of the four forms
// is one — every scalar of the value language is a JSON string, which is what
// makes a comparison of two answers string equality. [WriteValue] is the looser
// type, because a node can be an object or an array as well.
func WriteScalar(value reflect.Value) (string, error) {
	if held, ok := value.Interface().(*big.Int); ok {
		if held == nil {
			return "", fmt.Errorf("an item decoded into no integer at all")
		}

		return held.String(), nil
	}

	switch value.Kind() {
	case reflect.String:
		return strings.TrimRight(value.String(), " "), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// An unsigned binary item, which a generator gives an unsigned type
		// because the accessor that reads it returns one. The digits are the
		// digits either way: the value language has one spelling for a number
		// and it is the decimal one.
		return strconv.FormatUint(value.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		// A float32 widens to a float64 exactly, so the hexadecimal form of the
		// widened value is the hexadecimal form of what the item holds.
		return FormatFloat(value.Float()), nil
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return base64.StdEncoding.EncodeToString(value.Bytes()), nil
		}
	}

	return "", fmt.Errorf("an item decoded into a %s, which is not a value this harness can write", value.Type())
}

// exportedFields is the fields of a struct a caller of the generated package
// can see, in the order they are declared.
//
// The unexported ones are skipped rather than reported: the bytes retained for
// the slack nodes of a group are held in one, and they are not a decoded value.
func exportedFields(value reflect.Value) []reflect.Value {
	var fields []reflect.Value

	for i := range value.NumField() {
		if value.Type().Field(i).IsExported() {
			fields = append(fields, value.Field(i))
		}
	}

	return fields
}

// nodeName is what the copybook calls a node, falling back to its identifier so
// that a diagnostic about a node the descriptor has no name for still says
// which one it was. The key a values document writes is [original]'s.
func nodeName(node *irpb.Node) string {
	if name := original(node); name != "" {
		return name
	}

	return fmt.Sprintf("node %d", node.GetId())
}
