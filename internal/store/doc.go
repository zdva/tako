// Package store persists Tako's JSON documents (hosts, settings, session) on
// disk.
//
// Every document is a JSON object with a top-level "version". Writes are
// atomic (temp file, fsync, rename) with owner-only permissions. Reading an
// older version backs the file up and migrates it step by step; reading a
// newer version is refused so an old Tako never overwrites newer data. A
// corrupt file is set aside, never silently discarded, and the newest backup
// is used instead.
package store
