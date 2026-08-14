// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package goadapter is the conformance adapter for cpybkc-gen-go: a program
// that speaks docs/adapter/SPEC.md over its own standard input and standard
// output, and answers what the generated Go code made of a corpus entry's bytes
// (#200).
//
// It is an adapter and not a runner, and the difference is the whole point of
// the package. A runner is something
// [github.com/Zaba505/cpybkc/internal/conformance/engine] would have to know
// about; an adapter is a process the engine starts and speaks to, exactly as it
// speaks to one somebody else wrote in another language. cpybkc's own generator
// is driven through the public contract for the same reason a library's own
// tests use its public API: if the contract cannot carry this generator, it
// cannot carry a third party's either, and the cost of learning that here is one
// refactor.
//
// # What a conversation does
//
//  1. hello declares this adapter: kind codec, and the write capability,
//     because cmd/cpybkc-gen-go emits a writer.
//  2. generate is handed every entry's descriptor at once. Each one goes to the
//     generator through [github.com/Zaba505/cpybkc/internal/plugin], so the
//     argument vector is the one docs/plugin/SPEC.md fixes, and a codec program
//     is written beside what came back. All of them are then compiled in one
//     invocation of the Go toolchain, which is what generate carrying the whole
//     corpus is for.
//  3. decode starts that entry's codec program on the bytes the frame carried
//     and answers with the values document it wrote.
//  4. roundtrip tells the same still-running process to lay the records its
//     reader produced back out, read that file, and answer with what came back.
//  5. bye stops the process, removes the scratch tree and exits zero.
//
// # Why the codec program outlives its decode
//
// roundtrip carries no records, and it cannot: docs/conformance/SPEC.md's
// "Slack is not a value" puts bytes on a record that never appear in a values
// document, so records rebuilt from one are missing exactly the bytes the
// writing direction is being asked about. The records have to be the reader's
// own — which here means they have to stay inside the process that read them.
//
// So a decode leaves that process alive, holding its records and waiting on one
// word, and a roundtrip is that word. It is killed by the next decode, by bye,
// or by this adapter exiting, which is the retention docs/adapter/SPEC.md asks
// for and no more: one entry's records, until the next decode or the end of the
// conversation.
//
// Answering roundtrip out of a document computed during decode would have been
// simpler and would have been a lie of the kind this contract exists to catch —
// a report saying the writing direction was exercised when it was asked for,
// about a writer that ran while nobody was asking.
//
// # How a Go type is paired with a record node
//
// A values document names a record as the copybook spells it, and the generated
// reader produces a struct whose name is an identifier cmd/cpybkc-gen-go munged
// out of that name. Something has to pair them, and this adapter pairs them by
// folding both — every letter and digit kept, lowered, everything else dropped —
// and requiring exactly one record node per folded name.
//
// That is deliberately the weakest property of the munging rule that can still
// pair a name with an identifier. It does not know about casing, initialisms or
// separators, so it cannot drift when the generator's opinion of any of them
// changes, and it is checked rather than assumed: a fold that matches no record
// node, or two that match one, stops the entry with a diagnostic instead of
// walking the wrong record.
//
// What it replaces is a parse of the generated source that paired the structs
// with the record nodes by *position*, resting on a sentence in
// cmd/cpybkc-gen-go's README about the order they are emitted in. That pairing
// was load-bearing and unenforced: a generator that emitted them in another
// order would have compared each record against the wrong node, and nothing in
// the corpus would have said so. This one is the adapter's own business, needs
// no promise from the generator's output beyond the letters in a name, and
// fails loudly when it does not hold.
//
// # Where the scratch tree goes, and why it is inside the repository
//
// A generated package imports github.com/Zaba505/cobol-go/codec, so it has to be
// built somewhere a go.mod already requires it. Building it inside this module
// costs nothing and needs no network; building it outside would need a module, a
// requirement and a checksum for every dependency the generated code pulls in,
// resolved while the run is in flight.
//
// So the tree goes under this package's own directory, in a directory whose name
// begins with an underscore. The go tool ignores such a directory when it
// matches ./..., which is what keeps a scratch package out of `go build ./...`,
// `go vet ./...` and the linter, while an explicit path still builds it. Where
// the repository is reaches this adapter as an argument
// ([Adapter.Root]) and is refused when it is empty, so nothing here reaches a
// directory the machine happened to name (#184).
package goadapter
