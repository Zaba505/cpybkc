// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package emit writes a resolved IR descriptor where the CLI's --emit-ir flag
// names: in the protobuf binary wire encoding a generator plugin is handed, or
// — where --emit-ir-format asks for it — as the JSON a person reads.
//
// # Why the flag exists
//
// The layout parser and the resolver live in the CLI, so a third party who
// wants what they produce has exactly two ways to get it: write a generator
// plugin, or ask the CLI for the descriptor and read it directly. --emit-ir is
// the second, and it is the supported one — the reuse path for a consumer that
// is not a code generator at all, and the tool's primary debugging surface when
// a generator's output is wrong and the question is whether the IR or the
// generator is at fault.
//
// It is stable in the sense that matters to somebody building on it: the flag
// writes a [github.com/Zaba505/cpybkc/irpb.Descriptor] and nothing else, at the
// version that message carries, encoded the one way this package encodes the
// [Format] it was asked for.
// What is *in* a descriptor is docs/ir/SPEC.md's to say and its own version
// field's to move; a consumer whose build has no protobuf code generation in it
// decodes the bytes with the published FileDescriptorSet
// ([github.com/Zaba505/cpybkc/irpb.FileDescriptorSet]) and needs nothing from
// this repository but the two artifacts a release attaches.
//
// # A path, and - for standard output
//
// The operand is a filesystem path, or [Stdout] — "-", POSIX's spelling for a
// standard stream as an operand. Both are needed and neither is a convenience
// over the other.
//
// A path is what lets a plugin author run a plugin by hand against the exact
// bytes the CLI would have handed it. The file outlives the invocation that
// wrote it, so a failing generation can be replayed against a descriptor
// captured months earlier, and the answer is about those bytes rather than
// about a pipeline nobody can reproduce.
//
// Standard output is what gets a descriptor out of a container without a bind
// mount. `--emit-ir -` redirected on the host needs no writable volume, no
// matching UID on an output directory and no second command to copy the file
// back out — the difference between one `docker run` and a mount whose
// ownership is the caller's problem to get right, on an image that deliberately
// does not run as root.
//
// What standard output asks of its caller is that nothing else writes to that
// stream while a descriptor is going to it. Diagnostics belong on stderr
// whatever the destination is; a progress line interleaved into the wire
// encoding produces a descriptor that fails to decode at whatever field the
// line happened to land in, rather than anywhere near the mistake.
//
// # Determinism
//
// [Marshal] is the only place this repository encodes a descriptor for a
// consumer, and it encodes deterministically: equal descriptors produce equal
// bytes, so a consumer that diffs two emissions is reading a change to the
// resolved layout and never a change of encoder mood. Ordering *inside* the
// message — the node identifiers and the order of the node list — is the
// producer's obligation and is stated as one by docs/ir/SPEC.md, "Identity,
// ordering and determinism"; this package cannot supply it and does not pretend
// to. What it supplies is the other half, so that identical descriptors cannot
// come out as different files.
//
// The IR schema has no map field today, which is the one construct whose
// encoding would otherwise follow Go's randomised map iteration. Setting the
// option anyway is what keeps that a property of the encoder rather than a
// silent requirement on the schema, discovered by whoever adds the first map
// and diffs two runs.
//
// [MarshalJSON] owes the same promise and pays more for it; see "Normalized,
// because protojson is not" below.
//
// # Two forms, and which one is canonical
//
// The binary encoding is canonical. It is what a plugin is handed
// (docs/plugin/SPEC.md), what a consumer decodes, and what every requirement
// docs/ir/SPEC.md makes of a descriptor is a requirement about. [FormatJSON]
// asks for protojson instead, and that form is debug and interop output: for
// reading a descriptor beside the specification when a generator's output is
// wrong, and for the consumer whose language has protobuf tooling too weak to
// decode the binary form comfortably.
//
// The JSON is one-way. Nothing in this repository reads it back, no plugin is
// handed one, and this package will not decode one — a plugin accepting both
// would have to sniff which it had, which is the position docs/plugin/SPEC.md
// takes and the reason the two forms are not interchangeable. So the JSON is
// not a second wire encoding that the descriptor's version field governs, and a
// descriptor reconstructed from a rendering is nobody's guarantee.
//
// The default is therefore the canonical form. A caller who says nothing gets
// the bytes a plugin is handed, and the debug form is the one you have to ask
// for by name.
//
// # Normalized, because protojson is not
//
// [MarshalJSON]'s bytes are a function of the descriptor and of nothing else —
// in particular, not of the build that produced them. protojson deliberately
// varies its insignificant whitespace, emitting an extra space after a comma or
// after a "key:" chosen by a hash of the running binary, precisely so that
// nobody depends on its exact output. That is fixed within one cpybkc and
// different in the next, so a rendering committed beside a layout would diff on
// every line after an upgrade with nothing in it that anybody changed.
//
// So the rendering is re-indented through encoding/json before it is returned,
// which rewrites every byte of whitespace between tokens and leaves the token
// sequence alone. The output is then pinned to what protojson *said* rather
// than to how it spaced it. Everything else protojson already guarantees:
// fields come in field-number order, and the schema has no map field whose keys
// would need sorting.
//
// The property cannot be observed by rendering twice in one test binary — the
// spacing is chosen once per process — which is why the normalization is tested
// directly, against both shapes protojson emits, written out by hand.
//
// # It runs no generator
//
// Emitting is a terminal action, not a stage of generating. Nothing here loads
// a plugin, resolves a name on PATH or starts a process, and a caller that was
// asked for a descriptor and nothing else is finished once [Write] returns. The
// obligation that survives into the CLI is the same one stated the other way
// round: when --emit-ir is the only action requested, the command exits after
// it rather than going on to invoke a generator whose output nobody asked for.
package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/irpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	// Flag is the name of the CLI flag this package is behind, without its
	// leading dashes.
	//
	// It is spelled once, here, because the flag's name appears in the command
	// that defines it, in the error a bad operand produces and in the tests
	// that drive both. Three literals would be three places for it to be
	// renamed in two of.
	Flag = "emit-ir"

	// Stdout is the --emit-ir operand asking for the descriptor on standard
	// output rather than in a file.
	//
	// POSIX's Utility Conventions give "-" this meaning as an operand, which is
	// the same reading the plugin contract's --descriptor takes (docs/plugin/
	// SPEC.md). A dash is therefore never a relative path here, and a caller
	// wanting a file of that name spells it "./-".
	Stdout = "-"

	// FormatFlag is the name of the CLI flag selecting which [Format] --emit-ir
	// writes, without its leading dashes.
	//
	// It is a flag of its own rather than a value of --emit-ir, and the reason
	// is that --emit-ir's operand is already spoken for: it is the destination
	// (#20), so `--emit-ir=json` names a file called "json" and cannot also name
	// an encoding. A format and a destination are two answers, so they are two
	// flags — which is also what lets either form go to either destination
	// rather than making JSON a synonym for standard output.
	FormatFlag = "emit-ir-format"
)

// Format is the encoding --emit-ir writes a descriptor in.
//
// The set is closed, and closed by the same argument docs/ir/SPEC.md closes the
// node kinds with: a caller that meets an encoding this package does not
// produce should be told so, rather than handed the default and left to work
// out later which bytes it got.
//
// It satisfies flag.Value, so a command binds the flag with flag.Var and the
// rejection of an unknown spelling happens once, here, at parse time.
type Format string

const (
	// FormatBinary is the protobuf binary wire encoding: the canonical form, the
	// bytes a plugin is handed, and what --emit-ir writes when nothing asks
	// otherwise.
	FormatBinary Format = "binary"

	// FormatJSON is protojson, normalized: debug and interop output, one-way,
	// and never what a plugin is handed. See the package documentation, "Two
	// forms, and which one is canonical".
	FormatJSON Format = "json"
)

// String returns the format as it is spelled on the command line, so that a
// usage message and an error report the name a caller would type.
func (f Format) String() string {
	return string(f)
}

// Set parses the --emit-ir-format operand, and is what makes an unrecognised
// encoding a parse failure rather than a silent fallback to [FormatBinary].
//
// A caller who asked for JSON and quietly received protobuf would find out at
// whatever reads the file, which is the wrong end of the pipeline to discover a
// misspelling from.
func (f *Format) Set(s string) error {
	switch parsed := Format(s); parsed {
	case FormatBinary, FormatJSON:
		*f = parsed

		return nil
	default:
		return fmt.Errorf("%q is not a format; write %q or %q", s, FormatBinary, FormatJSON)
	}
}

// Marshal encodes d in the protobuf binary wire encoding: the form a generator
// plugin is handed, the form --emit-ir writes by default, and the canonical
// form of a descriptor.
//
// The bytes are deterministic; see the package documentation for what that
// covers and what it leaves to the producer of the descriptor.
//
// A nil descriptor is an error. protobuf encodes one to zero bytes and reports
// no failure, which would put an empty file where a descriptor was asked for
// and leave the mistake to be found by whoever tries to decode it — a producer
// that failed to build a descriptor and did not say so is the one thing this
// package can still turn back into a failure.
func Marshal(d *irpb.Descriptor) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("there is no descriptor to encode")
	}

	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the descriptor: %w", err)
	}

	return b, nil
}

// MarshalJSON renders d as protojson, normalized: the debug and interop form
// [FormatJSON] asks for, and never the form a plugin is handed.
//
// The rendering uses protobuf's own field names — start_state_id, not
// startStateId — because a descriptor is read beside docs/ir/SPEC.md and
// proto/cpybkc/ir/v1/ir.proto, and a lowerCamelCase rendering would make the
// reader translate every name back before they could look one up.
//
// Two renderings of one descriptor are the same bytes, and so are two
// renderings by different builds of cpybkc; the package documentation,
// "Normalized, because protojson is not", says what that costs and why the
// obvious implementation does not provide it.
//
// The bytes end in exactly one newline, because unlike the binary form this one
// is a text file: it is committed, diffed and pasted into an issue, and a
// rendering without a final newline reports itself as a change in every diff
// that touches it and leaves a shell prompt mid-line.
//
// A nil descriptor is an error, for the reason [Marshal] gives — protojson
// renders one as "{}" without complaint, which is a rendering of nothing that
// looks like a rendering of an empty descriptor.
func MarshalJSON(d *irpb.Descriptor) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("there is no descriptor to render")
	}

	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("failed to render the descriptor as JSON: %w", err)
	}

	indented, err := indentJSON(b)
	if err != nil {
		return nil, err
	}

	return append(indented, '\n'), nil
}

// jsonIndent is the indentation a rendered descriptor is written with. Two
// spaces, which is what the layout format and the project manifest a reader
// meets in the same sitting are indented with.
const jsonIndent = "  "

// indentJSON reformats valid JSON into one canonical indented shape, discarding
// whatever insignificant whitespace it arrived carrying.
//
// This is the step that makes [MarshalJSON]'s output a function of the
// descriptor rather than of the binary that rendered it. It is a function of
// its own, and tested as one, because the property it supplies cannot be
// observed through protojson from inside a single test binary.
func indentJSON(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", jsonIndent); err != nil {
		return nil, fmt.Errorf("failed to normalize the rendered descriptor: %w", err)
	}

	return buf.Bytes(), nil
}

// encode encodes d in the format asked for.
//
// An unrecognised format is an error rather than a fallback to [FormatBinary].
// [Format.Set] already rejects one at parse time, so reaching here means a
// caller assembled a Format some other way, and the failure it is about to
// cause — a consumer handed bytes in an encoding it did not ask for — is worth
// less than the failure of being told.
func encode(d *irpb.Descriptor, format Format) ([]byte, error) {
	switch format {
	case FormatBinary:
		return Marshal(d)
	case FormatJSON:
		return MarshalJSON(d)
	default:
		return nil, fmt.Errorf("--%s: %q is not a format; write %q or %q", FormatFlag, format, FormatBinary, FormatJSON)
	}
}

// Write encodes d in format and writes it to dest, which is a filesystem path
// or [Stdout] — in which case the bytes go to out and out alone.
//
// The format is a parameter rather than a default this function supplies,
// because the two forms are not interchangeable (see the package documentation,
// "Two forms, and which one is canonical") and a caller that never says which
// one it wants is a caller that has not decided. The command line is where the
// default lives: --emit-ir-format is bound to [FormatBinary].
//
// An existing file is replaced. A descriptor is derived entirely from its
// inputs, so re-emitting after a layout changed is the whole point of the flag,
// and refusing the second run would make the ordinary case the one that needs a
// flag to get past.
//
// A path whose parent directory does not exist is an error rather than a
// directory this creates. The operand is a path somebody typed, where a missing
// parent is far more often a typo than an intention, and the caller that does
// want a tree made — a pipeline naming an output path in a container filesystem
// it owns — is the one place that knows it.
//
// Nothing is created or truncated until d has encoded, so a call that fails
// leaves whatever was at dest alone rather than replacing a good descriptor
// with a short one. A path is then written in full or not at all; see
// [writeFile].
func Write(dest string, out io.Writer, d *irpb.Descriptor, format Format) error {
	if dest == "" {
		return fmt.Errorf("--%s: name a file to write the descriptor to, or %q for standard output", Flag, Stdout)
	}

	b, err := encode(d, format)
	if err != nil {
		return err
	}

	if dest == Stdout {
		if _, err := out.Write(b); err != nil {
			return fmt.Errorf("failed to write the descriptor to standard output: %w", err)
		}

		return nil
	}

	return writeFile(dest, b)
}

// descriptorPerm is the mode a written descriptor carries.
//
// It is set explicitly rather than inherited from the temporary file below,
// which [os.CreateTemp] makes 0600: a descriptor is an ordinary output file that
// a later step of a pipeline, often running as another user, reads back, and a
// mode that depended on which of the two write paths produced the file would be
// a difference nothing in the run explains.
const descriptorPerm = 0o644

// writeFile puts b at dest in full or not at all.
//
// docs/cli/SPEC.md requires exactly that of a path destination — "a path is
// written in full or not at all, so that nothing partial is ever left where
// another tool would read it" — and truncating dest and writing into it does
// not provide it. A write interrupted halfway, by a full disk or by the run
// being cancelled, would leave a descriptor that is a prefix of a descriptor:
// bytes that decode as far as they go and then stop, which a consumer meets as a
// malformed message rather than as the failed emission it is, and which has
// already replaced whatever good descriptor was there before.
//
// So the bytes go to a temporary file beside dest and are renamed onto it. The
// rename is atomic within a directory, which is why the temporary is made there
// rather than in TMPDIR — a rename across filesystems is a copy, and a copy is
// the partial write this function exists to avoid.
//
// The temporary is removed on every path out. After a successful rename there is
// nothing at that name to remove and the attempt fails harmlessly, which is
// cheaper than tracking whether the rename happened.
func writeFile(dest string, b []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*")
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}

	name := temp.Name()

	defer func() { _ = os.Remove(name) }()

	if _, err := temp.Write(b); err != nil {
		_ = temp.Close()

		return fmt.Errorf("failed to write %s: %w", dest, err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}

	if err := os.Chmod(name, descriptorPerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}

	if err := os.Rename(name, dest); err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}

	return nil
}
