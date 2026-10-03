// Package liblzo2 wraps liblzo2, the reference LZO implementation, so tests and benchmarks can use it as a
// black-box oracle. It is only built with the liblzo2 build tag (and cgo), and is empty otherwise.
//
// liblzo2 is GPL-2.0+ licensed: this package must only ever be imported from tests, never from library code.
// Use it through its API only; do not read or port liblzo2's sources (see DEVELOPING.md).
package liblzo2
