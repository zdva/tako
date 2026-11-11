package workspace

import "sync/atomic"

// atomicTab is an atomically replaced *Tab.
type atomicTab struct{ p atomic.Pointer[Tab] }

func (a *atomicTab) Load() *Tab   { return a.p.Load() }
func (a *atomicTab) Store(t *Tab) { a.p.Store(t) }
