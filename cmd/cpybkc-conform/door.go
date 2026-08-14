// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"flag"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// sizes is a size as the container runtimes spell one: a number of bytes, with
// an optional unit. It is deliberately looser than either runtime's own parser
// — the runtime is the authority on its own notation, and this only has to
// catch the mistake that would otherwise be discovered as a corpus that faulted.
var sizes = regexp.MustCompile(`^[0-9]+[bkmgBKMG]?$`)

// chosen is what the flags said about the door, gathered so that the choice
// between the two is made in one place and reads as one decision.
type chosen struct {
	// The command door.
	exec string
	dir  string

	// The image door.
	image     string
	runtime   string
	memory    string
	scratch   string
	processes int
	timeout   time.Duration

	// deadline and buildDeadline are the engine's, and are here because the wall
	// clock has to outlive both. See [imageDoor].
	deadline      time.Duration
	buildDeadline time.Duration

	// args is the adapter's own argument vector, whichever door it goes
	// through.
	args []string
}

// chooseDoor is the door this run goes through: a command, or a container
// image.
//
// Exactly one, and neither is a default. The two provide very different things
// — the image door is where no network, a read-only root and a wall-clock bound
// live, and the command door provides none of them — so a run that fell back
// from one to the other would produce a result whose believability depended on
// something the caller never wrote down.
func chooseDoor(flags *flag.FlagSet, c chosen) (engine.Door, error) {
	// Which flags were written, rather than which hold a value: every flag
	// below has a default, so a caller who spelled one out and a caller who
	// left it alone are otherwise indistinguishable — and the whole point of
	// refusing a flag that belongs to the other door is to tell those two apart.
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })

	switch {
	case c.exec != "" && c.image != "":
		return nil, fmt.Errorf("--exec and --image are two doors and a run goes through one of them: --exec runs "+
			"the adapter here as a process, and --image runs it in a container with no network, a read-only root "+
			"and a wall-clock bound\n\n%s", usage)
	case c.exec == "" && c.image == "":
		return nil, fmt.Errorf("--exec or --image is required: name the adapter to run, either as an executable "+
			"here or as a container image\n\n%s", usage)
	case c.image != "":
		return imageDoor(set, c)
	default:
		return commandDoor(set, c)
	}
}

// commandDoor is --exec: the adapter as a process on this machine, with no
// isolation of any kind.
func commandDoor(set map[string]bool, c chosen) (engine.Door, error) {
	// The image door's flags are refused rather than ignored. Each of them
	// names a bound, and a caller who wrote one has said what they wanted the
	// run to guarantee; accepting it silently on the door that guarantees
	// nothing would hand them a report saying the opposite of what they asked
	// for, in a sentence they had no reason to re-read.
	for _, name := range []string{"runtime", "image-deadline", "image-memory", "image-processes", "image-scratch"} {
		if set[name] {
			return nil, fmt.Errorf("--%s is the image door's and this run goes through --exec, which provides no "+
				"isolation at all: write --image to run the adapter in a container, or drop the flag\n\n%s",
				name, usage)
		}
	}

	path, err := adapterPath(c.exec)
	if err != nil {
		return nil, err
	}

	return &engine.Command{Path: path, Args: c.args, Dir: c.dir}, nil
}

// imageDoor is --image: the adapter in a container, which is where the
// properties that make a result worth handing to somebody else live.
func imageDoor(set map[string]bool, c chosen) (engine.Door, error) {
	// --dir is the command door's. A container's working directory is the
	// image's, and there is no host directory for it to name: this door mounts
	// none, which is half of what "no filesystem access to the corpus" means
	// (docs/adapter/SPEC.md, "The bytes travel in the frame, and not as a
	// path").
	if set["dir"] {
		return nil, fmt.Errorf("--dir is the command door's and this run goes through --image: a container's "+
			"working directory is the image's, and this door mounts no directory of yours into it\n\n%s", usage)
	}

	if err := positive(c.timeout); err != nil {
		return nil, err
	}

	if c.runtime == "" {
		return nil, fmt.Errorf("--runtime names the container runtime to find on PATH, and an empty name is not "+
			"one: write which you meant, or leave the flag alone for %s\n\n%s", engine.DefaultRuntime, usage)
	}

	if c.processes < 1 {
		return nil, fmt.Errorf("--image-processes %d is not a cap: an adapter is at least one process", c.processes)
	}

	for _, size := range []struct{ flag, value string }{
		{flag: "--image-memory", value: c.memory},
		{flag: "--image-scratch", value: c.scratch},
	} {
		// Checked here rather than left to the runtime, which reports a size it
		// will not take by refusing to create the container — after the door has
		// opened, so what the operator reads is every entry faulting rather
		// than the flag they mistyped.
		if !sizes.MatchString(size.value) {
			return nil, fmt.Errorf("%s %q is not a size: write a number of bytes, optionally followed by b, k, m "+
				"or g, as your runtime spells it", size.flag, size.value)
		}
	}

	// A reference is refused here as well as in the door, so that the shape of
	// what was written is a usage error rather than a run that could not be
	// started. The reason it matters is the door's: see [engine.Image.Open].
	if strings.HasPrefix(c.image, "-") {
		return nil, fmt.Errorf("--image %q begins with a dash, and the reference goes where the container runtime "+
			"reads its own options: write the image you meant\n\n%s", c.image, usage)
	}

	// The two bounds whose wrong value is silent. The wall clock takes the whole
	// container away, while the engine's deadlines bound one operation each:
	// generate may legitimately run a compiler over the whole corpus under
	// --build-deadline, and every entry is asked under --deadline. A wall clock
	// shorter than either takes the adapter away in the middle of something it
	// was allowed to do, and the entry it was on faults for a reason that is the
	// door's rather than the generator's.
	for _, bound := range []struct {
		flag  string
		value time.Duration
	}{
		{flag: "--build-deadline", value: c.buildDeadline},
		{flag: "--deadline", value: c.deadline},
	} {
		if c.timeout <= bound.value {
			return nil, fmt.Errorf("--image-deadline %s is not longer than %s %s: the wall clock bounds the whole "+
				"container, so a shorter one ends the adapter in the middle of work this run allows it",
				c.timeout, bound.flag, bound.value)
		}
	}

	return &engine.Image{
		Reference: c.image,
		Args:      c.args,
		Runtime:   c.runtime,
		Timeout:   c.timeout,
		Memory:    c.memory,
		Processes: c.processes,
		Scratch:   c.scratch,
	}, nil
}
