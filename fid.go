package main

import "fmt"

type Fids struct {
	ids  *Idmap
	fids map[*Id]*Fid
}

var fids Fids

var fidgen uint64 = 0x100000000

func (fs Fids) String() string {
	s := ""
	for _, v := range fs.fids {
		s += fmt.Sprintf("%v\n", v)
	}
	return s
}

func lookuplfid(local uint64) *Fid {
	id := fids.ids.lookuplocal(local)
	if id != nil {
		return fids.fids[id]
	}
	return nil
}

func lookuprfid(remote uint32) *Fid {
	id := fids.ids.lookupremote(remote)
	if id != nil {
		return fids.fids[id]
	}
	return nil
}

func freefid(f *Fid) {
	id := f.fid
	fids.ids.freeid(id)
	delete(fids.fids, id)
}

func allocfid(lfid uint64) *Fid {
	id := fids.ids.allocid(lfid)
	if id == nil {
		return nil // duplicate fid, TODO: mustn't happen
	}
	fid := new(Fid)
	fid.fid = id
	fids.fids[id] = fid
	return fid
}

func (a *Fids) init() {
	a.ids = new(Idmap)
	a.ids.init()
	a.fids = make(map[*Id]*Fid)
}
