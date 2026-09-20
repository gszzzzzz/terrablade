# Fuzz corpus

The seed corpus lives in the `f.Add` calls of each fuzz target, next to the
code it exercises, so that a reader of the test sees the inputs it starts from.

A file checked in under `FuzzLex/`, `FuzzExpression/`, `FuzzParse/`, or
`FuzzBody/` is an addition to those seeds, and `go test` replays it on every
run. An entry earns its place here only when it is one of:

- a regression: the input reproduces a bug that was fixed, so replaying it
  keeps the fix honest; or
- coverage the seeds lack: the input reaches statements that no seed and no
  other checked-in entry reaches **when the fuzz targets run alone**. The unit
  tests cover most of the package on their own, so a whole-suite profile
  cannot tell whether a corpus entry carries its weight; only a fuzz-only
  profile can.

Bulk fuzzer output that meets neither test does not belong here. It costs
review attention and test time on every run without pinning any behavior, and
a fresh `go test -fuzz` run explores that space again anyway.

## Measuring

Build the package test binary once and replay only the fuzz targets, from this
package's directory so that `testdata/fuzz` is found:

	go test -c -cover -covermode=set -coverpkg=./internal/syntax/ -o /tmp/syntax.test ./internal/syntax/
	cd internal/syntax && /tmp/syntax.test -test.run '^Fuzz' -test.coverprofile=/tmp/fuzz.out
	go tool cover -func=/tmp/fuzz.out | tail -1

The current corpus reaches **93.0% of statements (829/891) with the fuzz
targets alone**; the seeds plus the regression entries alone reach 91.4%
(814/891). Removing any checked-in entry that is not a regression should drop
that number — if it does not, the entry is no longer earning its place.

To record a new discovery, copy the file the fuzzer wrote under
`$GOCACHE/fuzz/` into the target's directory here, and say in the commit
message which of the two reasons above it satisfies. For a coverage entry, say
which blocks it adds to the fuzz-only profile.
