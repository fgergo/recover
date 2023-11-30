package main

import (
	"fmt"
	"sync"
)

type Id struct {
	local  uint64 /* local id */
	remote uint32 /* remote id */
}

func (id Id) String() string {
	return fmt.Sprintf("local: [%d], remote: [%d]", id.local, id.remote)
}

type Idmap struct {
	local2id  map[uint64]*Id
	remote2id map[uint32]*Id
	v         [MAXFID / UINT32BITS]uint32
	sync.RWMutex
}

func (idmap *Idmap) String() string {
	s := ""
	for i := len(idmap.v) - 1; i >= 0; i-- {
		s += fmt.Sprintf("%08b ", idmap.v[i])
	}
	return s
}

// lookuplocal(local) returns (local, remote) Id pair by local
func (idmap *Idmap) lookuplocal(local uint64) *Id {
	idmap.RLock()
	defer idmap.RUnlock()

	if id, ok := idmap.local2id[local]; ok {
		return id
	}

	log("lookuplocal('%v') == nil", local)
	return nil
}

// lookupremote(remote) returns (local, remote) id pair by remote
func (idmap *Idmap) lookupremote(remote uint32) *Id {
	idmap.RLock()
	defer idmap.RUnlock()

	if id, ok := idmap.remote2id[remote]; ok {
		return id
	}

	log("lookupremote('%v') == nil", remote)
	return nil
}

// must be called only from allocid()
func (idmap *Idmap) allocremote() uint32 {
	var i int
	var bit uint32

	for i = 0; i < len(idmap.v); i++ {
		if idmap.v[i] != ^uint32(0) {
			break
		}
	}

	must(i < len(idmap.v), "out of rfids")

	bit = ^idmap.v[i]
	bit &= -bit /* grab lowest bit */
	idmap.v[i] ^= bit
	must(bit != 0, "bit == 0")
	for i = i * 8 * UINT32BITS; (bit & 1) == 0; i++ {
		bit >>= 1
	}

	return uint32(i)
}

// TODO: later, consider returning only remote (uint32) instead of *Id
func (idmap *Idmap) allocid(local uint64) *Id {
	idmap.Lock()
	defer idmap.Unlock()

	if _, exists := idmap.local2id[local]; exists {
		dumplast()
		logfatal("allocid(), error: duplicate local id: %v\n", local)
	}

	remote := idmap.allocremote()
	id := new(Id)
	id.local = local
	id.remote = remote
	idmap.local2id[local] = id
	idmap.remote2id[remote] = id
	return id
}

func (idmap *Idmap) freeid(id *Id) {
	p1 := idmap.lookuplocal(id.local)
	must(p1 != nil, "id.local does not exist")
	p2 := idmap.lookupremote(id.remote)
	must(p2 != nil, "id.remote does not exist")
	must(p1 == p2, "id.local != id.remote") // can't happen

	idmap.Lock() // TODO, later: sure no race?
	defer idmap.Unlock()

	must(id.remote < MAXFID, "remote >= MAXFID")

	// free remote tag
	i := uint32(id.remote / (8 * UINT32BITS))
	bit := uint32(1) << (id.remote % (8 * UINT32BITS))
	idmap.v[i] &= ^bit

	// delete last reference to id
	delete(idmap.local2id, id.local)
	delete(idmap.remote2id, id.remote)
}

func (idmap *Idmap) init() {
	idmap.local2id = make(map[uint64]*Id)
	idmap.remote2id = make(map[uint32]*Id)
}
