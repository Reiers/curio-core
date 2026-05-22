// Package pdptests holds Curio Core's compile-only ports of upstream
// Curio PDP test files. They're gated behind the build tag
// `pdp_full_carveout` because the transitive test-time deps pull in
// `lotus/storage/paths` (which has the same CGo problem we patched in
// our Reiers/curio fork) and `elastic/gosigar` (a third-party dep
// that uses CGo for darwin sysctlbyname). Until those are also patched,
// these tests can't compile under CGO_ENABLED=0.
//
// To activate the tests:
//   go test -tags pdp_full_carveout ./internal/pdptests/
//
// Today this surfaces the next layer of carveout work needed before
// PDP unit tests can run inside curio-core. Tracked in the project
// PLAN.md as Day 7+ work.

package pdptests
