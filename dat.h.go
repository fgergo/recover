package main

import (
	llog "log"
	"os"

	"9fans.net/go/plan9"

	"fmt"
	"time"
)

const (
	MAXFID         = 4096   // maximum remote fid used
	UINT32BITS     = 8 * 32 // nee: 8*sizeof(ulong)
	NHASH          = 128
	REDIAL_TIMEOUT = 1 * time.Second
)

type Attach struct { // one per attach specifier
	link    *Attach
	aname   string    // attach specifier
	rootfid *Fid      // root fid of attach, internal use only
	rootqid plan9.Qid // root qid of attach
	gen     int       // gen. number of connection that saw last successful attach
	rgen    int       // gen. number of connection that last started to recover this
	mcount  uint32    // mount id for authenticators
}

func (a Attach) String() string {
	return fmt.Sprintf("[[Attach: aname: '%v',  rootfid.cname: %v, rootfid.sname: %v, rootqid: {%v}, gen: %v, rgen: %v, mcount: %d]]",
		a.aname, a.rootfid.cname, a.rootfid.sname, a.rootqid, a.gen, a.rgen, a.mcount)
}

type Fid struct { /* one per fid */
	fid       *Id
	gen       int    /* gen. number of connection that saw last successful fid use */
	rgen      int    /* gen. number of connection that last started to recover fid */
	cname     string /* what local fid corresponds to */
	sname     string /* what remote fid currently corresponds to (goal is cname) */
	attach    *Attach
	isopen    bool
	omode     uint8
	orclose   bool   /* remove on close? */
	dirscount uint32 /*point reading dir in server, goes to 0 when redial in internal Rwalk*/
	dirccount uint32 /*point reading dir in client, stays between redials*/
	fidtype   uint8  // nee type
	vers      uint32
}

func (f Fid) String() string {
	s := fmt.Sprintf("fid.fid: {%v}", f.fid) + "\n\t"
	s += fmt.Sprintf("fid: gen: %v, rgen: %v", f.gen, f.rgen) + "\n\t"
	s += fmt.Sprintf("fid: cname: %v, sname: %v", f.cname, f.sname) + "\n\t"
	s += fmt.Sprintf("fid: attach: {%v}", f.attach) + "\n\t"
	s += fmt.Sprintf("fid: isopen: %v, omode: %v, orclose: %v, dirscount: %v, dirccount: %v, fidtype: %0b, vers: %v", f.isopen, f.omode, f.orclose, f.dirscount, f.dirccount, f.fidtype, f.vers) + "\n"

	return s
}

type Req struct {
	tag      *Id
	fid      *Fid /* analogues of Fcall fields fid ≡ lookupfid(fcall.fid), etc. */
	newfid   *Fid
	internal bool        /* was this generated internally as part of fid recovery? */
	isattach bool        /* is this a Tattach message in Tclone's clothing? */
	isclunk  bool        /* is this a Tclunk message in Tremove's clothing? */
	flushing bool        /* have we sent a Tflush trying to flush this tag? */
	fcall    plan9.Fcall /* actual Fcall we want to satisfy fids are remote */
	gen      int
}

func (r Req) String() string {
	s := fmt.Sprintf("req: tag: {%v}", r.tag) + "\n\t"
	if r.fid != nil {
		s += fmt.Sprintf("req.fid.fid: %v", r.fid.fid) + "\n\t"
	} else {
		s += fmt.Sprintf("req.fid == nil") + "\n\t"
	}
	if r.newfid != nil {
		s += fmt.Sprintf("req.newfid.fid: %v", r.newfid.fid) + "\n\t"
	} else {
		s += fmt.Sprintf("req.newfid == nil") + "\n\t"
	}
	s += fmt.Sprintf("req: internal: %v, isattach: %v, isclunk: %v, flushing: %v", r.internal, r.isattach, r.isclunk, r.flushing) + "\n\t"
	s += fmt.Sprintf("fcall: {%v}", &r.fcall) + "\n\t"
	s += fmt.Sprintf("gen: %v", r.gen) + "\n"

	return s
}

func must(b bool, r string) {
	if !b {
		logfatal("assertion failed, reason: %v", r)
	}
}

func chat(format string, a ...interface{}) {
	if *flag_chatty {
		fmt.Printf(format+"\n", a...)
	}
}

func log(format string, a ...interface{}) {
	llog.Printf(format, a...)
}

func logfatal(format string, a ...interface{}) {
	log(format, a)
	os.Exit(1)
}
