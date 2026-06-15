// Minimal CGO-free replacement for github.com/elastic/gosigar (curio-core#80).
// Wired in via a replace directive in the root go.mod. See sigar.go for why.
module github.com/elastic/gosigar

go 1.26

require golang.org/x/sys v0.44.0
