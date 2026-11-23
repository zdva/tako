// Package model holds Tako's plain data types: hosts, how to authenticate to
// them and how to reach them.
//
// The package has no behavior beyond validation and normalization, and it
// imports nothing but the standard library, so every other package may depend
// on it. Values are safe to copy; slices inside them are owned by the value.
// Secrets never appear here: an [Auth] only carries a reference into the
// encrypted store.
package model
