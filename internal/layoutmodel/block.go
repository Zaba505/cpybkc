// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// Block is one `definitions-block` form: a name, and the entry body it holds
// (docs/layout/SPEC.md, "The `definitions-block` form").
//
// The body is carried as the nodes the layout wrote and nothing has been read
// out of them. What an entry is, is
// [github.com/Zaba505/cpybkc/internal/definitions]' to read and `cobol-go`'s to
// judge, and a reader here that read entries would be a second reading of
// them for the two to disagree about. What this layer owns is the block's name
// and where it sits: a record names a block by it, and a block no record names
// is the record-definitions layer's fault.
type Block struct {
	// Pos is the `definitions-block` form.
	Pos layout.Pos

	// TagPos is the form's tag.
	TagPos layout.Pos

	// Name is the block's name, the layout's own identifier for it and never a
	// COBOL name.
	Name string

	// NamePos is the name itself.
	NamePos layout.Pos

	// Entries are the block's body, every node after the name, in the order
	// the layout writes them.
	Entries []layout.Node
}

// ReadBlocks reads the `definitions-block` forms out of a parsed layout, in the
// order the layout writes them.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one. What it enforces is the half of a block the layout decides on its own:
// that a block is a name over one or more entries, and that no two blocks
// define one name. A name defined twice is reported at the second definition,
// with a second span at the first, because either block is a good definition
// on its own and the adopter has to decide which of the two the name was
// meant for.
//
// A block no record names, and a record naming a block nobody defines, relate a
// block to a record and are [ReadRecords]' to report.
func ReadBlocks(file *layout.File) ([]Block, error) {
	var (
		faults  diag.List
		blocks  []Block
		defined = make(map[string]layout.Pos)
	)

	for _, form := range file.Forms {
		if form.Tag != tagDefinitionsBlock {
			continue
		}

		if len(form.Elements) < 2 {
			faults.Fail(&BlockFormError{Pos: form.Pos, Found: shortfall(form.Elements)})

			continue
		}

		name, ok := form.Elements[0].(layout.Symbol)
		if !ok {
			faults.Fail(&BlockFormError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])})

			continue
		}

		if first, already := defined[name.Value]; already {
			faults.Fail(&DuplicateBlockError{Pos: name.Pos, First: first, Block: name.Value})

			continue
		}

		defined[name.Value] = name.Pos

		blocks = append(blocks, Block{
			Pos:     form.Pos,
			TagPos:  form.TagPos,
			Name:    name.Value,
			NamePos: name.Pos,
			Entries: form.Elements[1:],
		})
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return blocks, nil
}
