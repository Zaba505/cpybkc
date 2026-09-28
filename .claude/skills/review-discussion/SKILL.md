---
name: review-discussion
description: Triage one cpybkc GitHub Discussion post and reply to it. The skill works out what the post really is (a question, a feature request, a user's mistake, a real cpybkc bug, or spam), researches it against the example and conformance corpora, the specs and a reproduction, and then posts a reply backed by evidence. It files stories for any bug it reproduces, and asks the repository owners to agree before a feature decision turns into stories. Use this whenever the user asks to "review", "triage", "answer" or "reply to" a discussion, names a discussion by number or URL, or asks what is waiting in Discussions. Skip it for issues and pull requests.
argument-hint: <discussion number or URL>
---

# review-discussion

Take **one** discussion from where its thread stands to the next reply it needs, then stop.

The reply is posted from the account `gh` is logged in as, which is an owner's account. That
fact shapes two rules below:

- **Every comment this skill posts ends with the marker `<!-- review-discussion -->`.** It
  does not render, but the API returns it. It is the only way to tell a proposal this skill
  made apart from a decision an owner typed under the same login.
- **An owner's unmarked comment overrides anything the skill proposed.** Follow it. Never
  argue with it in the thread. If you think the owner is wrong, say so to the user in the
  session.

## 0. Read the thread

With no argument, list the discussions that have no comments yet and ask which one to take:

```sh
gh api graphql -f query='{repository(owner:"Zaba505",name:"cpybkc"){discussions(first:50,orderBy:{field:CREATED_AT,direction:DESC}){nodes{number title category{name} comments{totalCount}}}}}' \
  --jq '.data.repository.discussions.nodes[] | select(.comments.totalCount==0) | "#\(.number) [\(.category.name)] \(.title)"'
```

Otherwise read the whole thread: the post, then every comment and every reply to a comment.

```sh
gh api graphql -F n=<number> -f query='query($n:Int!){repository(owner:"Zaba505",name:"cpybkc"){discussion(number:$n){id url title body category{name} author{login} createdAt
  comments(first:100){nodes{id url author{login} body createdAt replies(first:100){nodes{id author{login} body createdAt}}}}}}}'
```

Then read who the owners are. Don't hard-code the list, because it changes:

```sh
gh api repos/Zaba505/cpybkc/collaborators --jq '.[] | select(.role_name=="admin") | .login'
```

Work out where the thread stands before you do anything:

| Thread state | What to do |
| --- | --- |
| No marked comment yet | Start at step 1. |
| Your marked proposal is there and no owner has answered it | Report "awaiting owners" and stop. Don't post a nudge. |
| An owner has posted an unmarked comment after your proposal | Their comment decides it. Go to step 4 if they agreed, or reply by their decision if they chose something else. |
| The reporter has sent new information, such as a reproduction you asked for | Start at step 2 with that information. |

## 1. Categorize

Classify the post by **what it says**. The discussion category is only the reporter's guess.
#340 was filed under Bugs, and it described a mechanism that did not exist. #304 was filed
under General, and it reported a defect. Say which kind the post is, and why in one sentence,
before you research anything.

| Kind | How to recognise it | Path |
| --- | --- | --- |
| **Question** | The reporter asks how to do something, or what cpybkc does | General → Question |
| **Feature request** | The reporter asks for something cpybkc does not do. This includes a "bug" where cpybkc refuses exactly as the spec says, but the spec does not cover the file the reporter has | General → Feature request |
| **User bug** | The copybook or layout is wrong, or cpybkc is invoked wrongly, and cpybkc behaves as its spec says | Bug → User bug |
| **cpybkc bug** | cpybkc does something other than what its spec, README or generated docs promise. This includes a defect upstream in `Zaba505/cobol-go` or `z5labs/sexpr-go` that cpybkc passes through | Bug → cpybkc bug |
| **Spam** | Unrelated to cpybkc, promotional, or abusive | No reply. Report it to the user. Never delete, lock or hide it |
| **Anything else** | An announcement, a poll, a show-and-tell post or a research note | No reply. Report it to the user and stop |

You can't tell a user bug from a cpybkc bug until you know what the spec promises. If you
can't tell yet, do the cpybkc bug research first. A failed reproduction often turns out to be
the user's mistake.

## 2. Research

Every claim in the reply needs evidence you produced during this run, not something you
remember. Pin every link you gather to a commit, so it doesn't drift when `main` moves:

```
https://github.com/Zaba505/cpybkc/blob/<sha>/example/ledger/ledger.sexpr#L10-L24
```

### Question

The goal is an answer backed by concrete evidence.

- **Corpus first.** Look in `example/` (worked projects with their generated output) and
  `testdata/conformance/` (small entries with the right answer written down) for the
  construct the reporter asks about. A real file from the repository is better than a
  snippet you write. If nothing in the corpus covers the construct, say so. That gap is a
  finding in its own right.
- **Commands you ran.** Every `cpybkc` snippet in the answer must be one you ran. Say
  whether you ran it on the latest release (`git tag --sort=-v:refname | head -1`) or on
  `main` at a SHA. A generator is found on `PATH` by name, so build it into a throwaway
  `GOBIN` first:

  ```sh
  tmp=$(mktemp -d)
  GOBIN=$tmp/bin go install ./cmd/cpybkc-gen-go ./cmd/cpybkc-gen-graph
  PATH=$tmp/bin:$PATH go run ./cmd/cpybkc --manifest <dir>/cpybkc.json
  ```

  Use `go run github.com/Zaba505/cpybkc/cmd/cpybkc@<tag>` to answer about a release. Don't
  use `dagger call` for this from a git worktree, because it fails there (see
  `CONTRIBUTING.md`).
- **Rules come from the specs.** When the answer is a rule, cite the section of the spec
  that states it, such as `docs/layout/SPEC.md#<anchor>`.
- **COBOL sources.** Start with the *Governing sources* section of the relevant
  `docs/*/SPEC.md`. That is the IBM z/OS and Enterprise COBOL material the project already
  treats as normative. Compilers disagree on some points, such as `OCCURS DEPENDING ON`
  sliding and line-sequential `RECFM=V`. Where they do, say which compiler does what,
  the way the specs do. Don't name a winner.

### Feature request

The goal is a decision: does this belong in cpybkc's logic, or in the adopter's own code?
These are the tests past threads settled on:

- **Is it about what the bytes of a real file are?** Then it belongs to cpybkc. Any shape a
  real copybook and extract can take, under any compiler whose files are in scope, is
  cpybkc's to describe (#340, which led to #346). What an implementation costs decides *how*
  it is built, never *whether* it is in scope.
- **Does the copybook fail to determine one right answer?** Then it belongs to the adopter.
  #272 declined a Parquet generator for this reason. COBOL-to-Go has essentially one right
  answer per construct. COBOL-to-Parquet has many, and choosing among them is the adopter's
  domain.
- **Check the spec's *Out of Scope* section.** Each `docs/*/SPEC.md` has one. If the request
  is already listed there with a reason, that reason is the decision, unless the post brings
  a fact the reason did not consider.
- **Support fully or refuse uniformly.** "Supported for some layouts, refused for others" is
  not an acceptable decision (#378). Propose either full support or a refusal by a rule that
  does not depend on what the layout holds.
- **cpybkc runs on developer machines.** Big-endian *files* are in scope. A mainframe
  *host* is not.

If you need to ask the reporter something, ask only what their own layout needs. Never ask
a question whose answer would narrow what the format supports.

### User bug

1. Pin down the mistake. Show the line of the copybook, layout or command that is wrong,
   and the spec section that says why.
2. Then check whether cpybkc could have prevented it. Did the diagnostic name the real
   problem? Did `cpybkc init` scaffold the thing the user got wrong? Does a spec rule go
   unchecked until generation time? If cpybkc could have stopped the mistake, that is a
   candidate feature request. Test it against the feature-request questions above.

### cpybkc bug

Try to reproduce it. Build the smallest copybook, layout and manifest that shows the
failure, in a throwaway directory outside the repository. Run it on:

- the version the reporter names, or else the latest release, and
- `main` at its current SHA.

For each run, record the exact commands, the output, and the SHA or tag. If it reproduces,
trace it to the stage that is wrong: `resolve`, the IR, a generator, or `cobol-go`
upstream. Name the file and function. Also check whether a test or a conformance entry
should have caught it.

If it reproduces on the release but not on `main`, it is already fixed. File no story.
The reply says it ships in the next release, and names the fixing commit if you can find
it.

## 3. Reply

For a cpybkc bug you reproduced on `main`, do step 4 first, so the reply can name the
stories.

Draft the reply in a file. Show the draft to the user, and post it only after they approve.
The reply is public and goes out under their name.

Every reply leads with its verdict, puts the evidence inline (commands, output, pinned
links), states versions and SHAs, and ends with the marker.

| Kind | The reply |
| --- | --- |
| **Question** | The answer, then the evidence from step 2. If the thread is in the Q&A category, leave marking the answer to the asker. |
| **Feature request** | The proposed decision (cpybkc's logic or the adopter's), the tests that decided it, and what would change it. **@-mention every owner** and ask for their agreement. Say plainly that nothing will be filed until they agree. |
| **User bug** | What the mistake is, where it is, and the fix. If step 2 found a way cpybkc could have prevented it, add it as a proposal and @-mention every owner, as for a feature request. |
| **cpybkc bug, reproduced** | "Reproduced on <tag> and on `main` at <sha>", then the minimal reproduction, what goes wrong and where, the stories filed for it, and a workaround if an honest one exists. |
| **cpybkc bug, not reproduced** | What you ran and what happened, then a request for a reproduction. Ask for the copybook (or a minimal one with the same shape), the layout, the manifest, the exact command, the cpybkc version, and the output. |

To post, put the discussion `id` from step 0 into this command. To answer inside a
comment's thread, add `-F replyToId=<comment id>` and `$replyToId:ID` to the mutation.

```sh
gh api graphql -F discussionId=<id> -F body=@reply.md -f query='mutation($discussionId:ID!,$body:String!){addDiscussionComment(input:{discussionId:$discussionId,body:$body}){comment{url}}}'
```

## 4. Filing stories

Two paths reach this step:

- **A cpybkc bug you reproduced on `main`.** Reproducing it is the validation, so it needs
  no owner agreement. Come here before drafting the reply.
- **A feature decision**, including a prevention proposal from a user bug. It comes here
  only when every owner you @-mentioned has agreed in the thread, or when the owners
  replied with a decision of their own.

Then:

1. List everything that is traceable to the thread: each story, each missing corpus
   entry, and each item that deliberately gets no story, with the reason. Show this map to
   the user before you create anything.
2. File the stories with `.github/ISSUE_TEMPLATE/story.yaml` (label `story`). For each
   issue, set the project field that `.claude/backlog.json` names under `select.project`.
   Wire dependencies as native `blocked_by` edges. `gh issue create` does neither, so do
   both by hand and read them back. If the defect is upstream in `cobol-go` or
   `sexpr-go`, the story belongs in that repository, so ask the user before filing there.
3. For a bug, the reply from step 3 names the stories. For a feature decision, post a
   marked comment in the thread that lists them.

## 5. Report

End with one line: the discussion, the kind you assigned it, what you posted (with the URL)
or why you posted nothing, and what the thread now waits on.
