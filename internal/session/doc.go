// Package session manages conversation messages for a single
// session and their persistence. A session may be
// bound to a Store (SQLite) for write-through save/restore;
// unbound sessions are in-memory only (subagents, disabled
// persistence).
package session
