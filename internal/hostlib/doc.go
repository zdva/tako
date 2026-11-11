// Package hostlib is the host library: the machines the user added, their
// tags, and the operations the sidebar needs on them (add, edit, delete,
// search, group by tag).
//
// A Library is safe for concurrent use. It persists itself through a
// store.Saver (debounced, atomic) and keeps secrets out of its file: hosts
// only carry keychain references, and deleting a host deletes its secrets.
package hostlib
