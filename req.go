package main

import (
	"9fans.net/go/plan9"
	"path"
	"strings"
)

type Reqs struct {
	ids  *Idmap
	reqs map[*Id]*Req
}

var reqs Reqs

var taggen uint32 = 0x10000

func allocreq(tag uint32) *Req {
	id := reqs.ids.allocid(uint64(tag))
	must(id != nil, "allocreq(), allocid() == nil")
	req := new(Req)
	req.tag = id
	reqs.reqs[id] = req

	return req
}

func lookuplreq(local uint64) *Req {
	id := reqs.ids.lookuplocal(local)
	if id != nil {
		return reqs.reqs[id]
	}
	return nil
}

func lookuprreq(remote uint16) *Req {
	id := reqs.ids.lookupremote(uint32(remote))
	if id != nil {
		return reqs.reqs[id]
	}
	return nil
}

var last [256]plan9.Fcall // last 256 fcall calls
var nlast int             // overflow shouldn't matter

// dump last 256 fcall calls
func dumplast() {
	var i int

	i = nlast - 256
	if i < 0 {
		i = 0
	}

	for ; i < nlast; i++ {
		chat("dumplast: [%v] log %v", i, &last[i&(len(last)-1)])
	}
}

func freereq(r *Req) {
	id := r.tag
	reqs.ids.freeid(id)
	delete(reqs.reqs, id)
}

// for all requests in reqs.reqs apply function fn
func forallreqs(fn func(*Req, *Fid), fid *Fid) int {
	n := 0

	orig := make(map[*Id]*Req, len(reqs.reqs))
	for k, v := range reqs.reqs {
		orig[k] = v
	}

	// iterate on original map (when fn() modifies reqs.reqs)
	for _, req := range orig {
		if fn != nil {
			fn(req, fid)
			n++
		}
	}
	return n
}

func dump(r *Req, fid *Fid) {
	chat("dump(), r: %v", r)
}

func dumpreqs() {
	chat("outstanding requests:")
	n := forallreqs(dump, nil)
	chat("total: %v outstanding requests", n)
}

func packwalk(cname string, f *plan9.Fcall) {
	f.Wname = strings.SplitN(path.Clean(cname), "/", plan9.MAXWELEM)
}

func (a *Reqs) init() {
	a.ids = new(Idmap)
	a.ids.init()
	a.reqs = make(map[*Id]*Req)
}
