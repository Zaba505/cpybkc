// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package descriptive is the conformance adapter for a generator that is not a
// conformance subject: one that emits a diagram, a schema, a document or a
// copybook rather than code that reads a file (#201).
//
// The corpus tests codecs. An entry is bytes and the values those bytes decode
// to, so asking about one means handing a file to something that reads files,
// and cmd/cpybkc-gen-graph has nothing to hand it to — it never opens
// input.bin, and no amount of running it would produce a record. The same is
// true of every generator that emits a description rather than a reader.
//
// docs/adapter/SPEC.md, "kind, because not every generator is a conformance
// subject", is what lets such a generator say so: kind is declared at the
// handshake, before the adapter has been asked anything, and an adapter that
// declares descriptive is not applicable rather than failing. This package is
// the adapter half of that. The engine's half — sending it nothing but bye, and
// reporting the run as not applicable — is
// [github.com/Zaba505/cpybkc/internal/conformance/engine]'s.
//
// # What a conversation is
//
// Four frames, which is the whole of it:
//
//	→ {"id":1,"op":"hello","protocol":1}
//	← {"id":1,"ok":true,"protocol":1,"name":"cpybkc-gen-graph adapter",
//	   "kind":"descriptive","capabilities":{}}
//
//	→ {"id":2,"op":"bye"}
//	← {"id":2,"ok":true}
//
// Nothing here runs a generator, and that is not an omission. A descriptive
// adapter is never asked anything a generator could answer, so an adapter that
// started one would be starting it to throw its output away — and the point of
// this package is that the framework can decline a subject it cannot test
// without pretending to have tested it.
//
// # Why the codec operations are refused rather than attempted
//
// An engine MUST NOT send generate, decode, roundtrip or rebuild to an adapter
// that declared itself descriptive, so a correct engine never reaches them.
// They are refused anyway, with ok: false, because the alternative shapes are
// both untrue: attempting one would mean inventing an answer about a file
// nothing read, and exiting would report a broken adapter where nothing is
// broken. A refusal costs the entry it was asked about and says why, which is
// the one honest thing left to say to an engine that asked the wrong question.
//
// # What this package is deliberately not
//
// No oracle. What a descriptive generator should be held to instead — whether a
// descriptive track is worth having at all, and what would grade it — is an
// open question in discussion #193, and specifying one before deciding whether
// to have one would specify it twice. This package settles only the cheap half:
// such a generator can say what it is in one member of the first frame, and be
// told the truth about itself.
package descriptive
