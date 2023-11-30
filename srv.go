package main

import (
	"net"
	"path/filepath"
	"strings"
	"time"

	"9fans.net/go/plan9"
)

const (
	Ebadfid   = "unknown fid"
	Efidinuse = "fid already in use"
	Efileexcl = "exclusive files are blocked by recover"
	Enoauth   = "authentication not required"
)

/*
* send back to client
 */
func csend(f *plan9.Fcall) {
	last[nlast&(len(last)-1)] = *f
	nlast++
	err := plan9.WriteFcall(srvconn, f)
	if err != nil {
		logfatal("srvconn write error: %v\n*f=%v", err, *f)
	}

}

/*
 * send error to client
 */
func cerror(tag uint16, msg string) {
	var f plan9.Fcall

	f.Type = plan9.Rerror
	f.Tag = tag
	f.Ename = msg
	csend(&f)
}

/*
 * add elem to name
 */
func namewalk(name string, elem string) string {
	return filepath.Clean(name + "/" + elem)
}

/*
 * set name to the next element needed to turn sname into cname.
  * if sname == "/usr" and cname == "/usr/rob/lib/profile", then
 * name gets set to "rob".
*/
func nextelem(sname string, cname string) string {
	must(len(sname) <= len(cname), "len(sname) > len(cname)")
	must(sname != cname, "sname == cname")

	n := strings.Index(cname, sname)
	must(n >= 0, "n<0")

	if cname[n:n+1] == "/" {
		n++
	}
	m := strings.Index(cname[n:], "/")
	if m < 0 {
		return cname[n:]
	}
	return cname[n:m]
}

/*
 * print a request
 */
func dumpreq(r *Req, f *Fid) {
	chat("dumpreq(), req %v", r.fcall)
}

/*
 * connection died and has been redialed.  restart a previously-sent request.
 */
func restartreq(r *Req, notused *Fid) {
	if r.internal {
		/*
		 * internal messages are specific to the connection they
		 * are created for.  no need to repeat them on later connections.
		 * other code will do that if necessary.
		 */
		chat("	freeing request: internal, r: %v", r)
		freereq(r)
	} else if r.fcall.Type == plan9.Tclunk {
		/*
		 * hanging up the connection is the uber-clunk.
		 */
		r.fcall.Type++ // Rclunk
		r.fcall.Tag = uint16(r.tag.local)
		r.fcall.Fid = uint32(r.fid.fid.local)
		freefid(r.fid)
		csend(&r.fcall)
		freereq(r)
	} else if r.fcall.Type == plan9.Tflush {
		/*
		 * we were trying to cancel a request so just don't restart it.
		 * RSC 7/15/2005: I'm sure this is broken.  If flushr has
		 * already been restarted, freeing it isn't quite correct!
		 * Also it might break the forallreqs loop.
		 * Perhaps we need to discard the flushes in a separate pass.
		 */
		flushr := lookuprreq(r.fcall.Oldtag)
		if flushr != nil {
			switch flushr.fcall.Type {
			case plan9.Tattach:
				freefid(flushr.fid)
			case plan9.Twalk:
				if flushr.newfid != nil {
					freefid(flushr.newfid)
				}
			}
			freereq(flushr)
		}
		r.fcall.Type++ // Rflush
		r.fcall.Tag = uint16(r.tag.local)
		csend(&r.fcall)
		freereq(r)
	} else {
		/*
		 * if the fid is usable, retransmit the request.
		 * RSC 7/15/2005: otherwise do we need to queue it?
		 */
		if fidready(r.fid) {
			err := plan9.WriteFcall(netconn, &r.fcall)
			if err != nil {
				log("restartreq(), WriteFcall() error, err: %v\nr:%v", err, r)
			} else {
				chat("net <- (restarted) %v", r.fcall)
			}
		} else {
			chat("	r.fid not ready for request r: %v", r)
		}
	}
}

/*
 * redial the connection and get going again
 */
func redial() {
	chat("dialing: %v", dialstring)

	var err error
Retry:
	if netconn != nil {
		netconn.Close()
	}

	network := "tcp"
	s := strings.Split(dialstring, "!")
	address := s[0]
	if len(s) == 2 {
		network = s[0]
		address = s[1]
	}
	netconn, err = net.Dial(network, address)
	if err != nil {
		chat("redial() Dial() error: %v", err)
		if gen == 0 {
			logfatal("can't establish initial connection to %v", dialstring)
		}
		time.Sleep(REDIAL_TIMEOUT)
		goto Retry
	}
	chat("connected!")

	err = xversion()
	if err != nil {
		logfatal("xversion(), err: %v", err)
	}

	var a *Attach
	for a = attachlist; a != nil; a = a.link {
		if authattach(a) < 0 {
			goto Retry
		}

		a.rgen = gen + 1
		a.gen = gen + 1
		a.rootfid.gen = gen + 1
		a.rootfid.rgen = gen + 1
	}

	gen++
	nreq := forallreqs(restartreq, nil)
	if nreq != 0 {
		chat("restarted %v requests", nreq)
	}
}

/*
 * Queue/send a request to server
 */
func queuereq(r *Req) {
	fid := r.fid

	if (r.fcall.Type == plan9.Tread) && (fid != nil) && (fid.fidtype == plan9.QTDIR) { /* readdir special case */
		if (fid.dirscount < fid.dirccount) && (r.fcall.Offset != 0) {
			chat("	adjusting readdir, dirscount %ud, dirccount %ud", fid.dirscount, fid.dirccount)
			r.fcall.Offset -= uint64(fid.dirccount - fid.dirscount)
		}
	}

	r.fcall.Tag = uint16(r.tag.remote)
	if fid != nil {
		r.fcall.Fid = fid.fid.remote
	}
	if r.newfid != nil {
		r.fcall.Newfid = r.newfid.fid.remote
	}
	r.gen = gen

	if r.internal || fidready(r.fid) {
		last[nlast&(len(last)-1)] = r.fcall
		nlast++
		err := plan9.WriteFcall(netconn, &r.fcall)
		if err != nil {
			log("queuereq(), WriteFcall() error, err: %v\nr:%v", err, r)
		} else {
			chat("net <- %v", &r.fcall)
		}
	}
}

/*
 * is the fid f usable on the current connection?
 */
func fidready(f *Fid) bool {
	if f == nil {
		return true
	}

	if f.gen == gen { /* fid is known on this connection */
		return true
	}

	if f.rgen == gen { /*we will walk it later when we are attached*/
		return false
	}
	if attachready(f.attach) && f != f.attach.rootfid {
		//
		// RSC 7/15/2005: attachready might have changed f->gen and f->rgen, no?
		//
		r := allocreq(taggen)
		taggen++
		r.fid = f.attach.rootfid
		r.newfid = f
		r.internal = true
		r.fcall.Type = plan9.Twalk
		packwalk(f.cname, &r.fcall)
		f.rgen = gen
		queuereq(r)
	}

	return false
}

/*
 * is the attach point a usable on this connection?
 */
func attachready(a *Attach) bool {
	if a.gen == gen {
		return true
	}

	if a.rgen == gen {
		return false
	}

	a.rgen = gen
	r := allocreq(taggen)
	taggen++
	r.internal = true
	r.fid = a.rootfid
	r.fcall.Type = plan9.Tattach
	r.fcall.Tag = uint16(r.tag.remote)
	r.fcall.Fid = r.fid.fid.remote
	r.fcall.Afid = plan9.NOFID
	r.fcall.Uname = eve
	r.fcall.Aname = a.aname
	queuereq(r)

	return false
}

/*
 * find the attachment for the given specifier, new one if necessary
 */
func attachment(spec string) *Attach {
	for a := attachlist; a != nil; a = a.link {
		if a.aname == spec {
			return a
		}
	}

	// create new attachment
	a := new(Attach)
	a.aname = spec
	f := allocfid(fidgen)
	fidgen++
	a.rootfid = f
	f.fidtype = plan9.QTDIR
	f.dirscount = 0
	f.dirccount = 0
	f.attach = a
	f.cname = "/"
	f.sname = "/"
	a.link = attachlist
	attachlist = a

	return a
}

func listensrv() {
	f, err := plan9.ReadFcall(srvconn)
	for err == nil {
		thelock.Lock()
		last[nlast&(len(last)-1)] = *f
		nlast++

		chat("srv -> %v", f)
		switch f.Type {
		case plan9.Tauth:
			f.Type = plan9.Rerror
			cerror(f.Tag, Enoauth)
		case plan9.Tflush:
			r := lookuplreq(uint64(f.Oldtag))
			if r == nil {
				f.Type++
				csend(f)
				break
			}
			oldtag := r.tag.remote
			r.flushing = true
			r = allocreq(uint32(f.Tag))
			r.fcall.Type = plan9.Tflush
			r.fcall.Oldtag = uint16(oldtag)
			queuereq(r)

		case plan9.Tattach:
			nfid := allocfid(uint64(f.Fid))
			if nfid == nil {
				cerror(f.Tag, Efidinuse)
				break
			}

			a := attachment(f.Aname)
			r := allocreq(uint32(f.Tag))
			r.fcall.Type = plan9.Twalk
			r.newfid = nfid
			r.fid = a.rootfid
			r.isattach = true
			chat("	queuing %v", r.fcall)
			queuereq(r)

		case plan9.Tversion:
			f.Type++
			f.Version = "9P2000"
			f.Msize = currmsize
			csend(f)

		case plan9.Twalk:
			var r *Req
			fid := lookuplfid(uint64(f.Fid))
			if fid == nil {
				cerror(f.Tag, Ebadfid)
				break
			}
			nfid := allocfid(uint64(f.Newfid))
			if nfid == nil {
				goto Walknotclone
			}
			r = allocreq(uint32(f.Tag))
			r.fcall = *f
			r.fid = fid
			r.newfid = nfid
			queuereq(r)
			break

			//		case Twalk:
			//		case Topen:
			//		case Tcreate:
			//		case Tread:
			//		case Twrite:
			//		case Tclunk:
			//		case Tremove:
			//		case Tstat:
			//		case Twstat:
		Walknotclone:
			fallthrough
		default:
			fid := lookuplfid(uint64(f.Fid))
			if fid == nil {
				cerror(f.Tag, Ebadfid)
				break
			}
			/*
			 * We can't recover ORCLOSE files.  Instead we drop the ORCLOSE
			 * flag and perform the remove at close ourselves.
			 */
			if f.Type == plan9.Topen || f.Type == plan9.Tcreate {
				fid.orclose = (f.Mode & plan9.ORCLOSE) == plan9.ORCLOSE
				f.Mode &= ^uint8(plan9.ORCLOSE)
			}
			if f.Type == plan9.Topen && (fid.fidtype&plan9.QTEXCL) == plan9.QTEXCL {
				cerror(f.Tag, Efileexcl) // Exclusive open use files not allowed.
				break
			}

			r := allocreq(uint32(f.Tag))
			if f.Type == plan9.Tclunk && fid.orclose {
				f.Type = plan9.Tremove
				r.isclunk = true
			}
			r.fcall = *f
			r.fid = fid
			queuereq(r)
		}
		thelock.Unlock()
		f, err = plan9.ReadFcall(srvconn)
	}
	log("Exiting listensrv, plan9.ReadFcall(srvconn), err: %v", err)
	log("socket '%v' exists?", srvname)
	exitch <- true
}

/*
 * copy parameters from old to new
 */
func clonefid(old *Fid, new *Fid) {
	new.cname = old.cname
	new.sname = old.sname
	new.gen = old.gen
	new.attach = old.attach
}

func externalresponse(r *Req, f *plan9.Fcall) {
	cf := *f
	cf.Tag = uint16(r.tag.local)

	if r.fid != nil {
		if f.Type == plan9.Rread && (r.fid.fidtype&plan9.QTDIR) == plan9.QTDIR {
			r.fid.dirscount += f.Count
		} else {
			r.fid.dirscount = 0
			r.fid.dirccount = 0
		}
		if r.fid.dirscount > r.fid.dirccount {
			r.fid.dirccount = r.fid.dirscount
		}

		if f.Type != plan9.Rerror {
			cf.Fid = uint32(r.fid.fid.local)
		}
	}

	if cf.Type == plan9.Rattach {
		log("unexpected Rattach r.fcall: %v, *f: %v", r.fcall, *f)
		dumplast()
	}

	if r.isattach && cf.Type != plan9.Rerror {
		must(cf.Type == plan9.Rwalk, "cf.Type != plan9.Rwalk")
		cf.Type = plan9.Rattach
		cf.Qid = r.fid.attach.rootqid //BUG, dont change specs...
	}
	if r.isclunk {
		must(cf.Type == plan9.Rremove || cf.Type == plan9.Rerror, "cf.Type != plan9.Rremove && cf.Type != plan9.Rerror")
		cf.Type = plan9.Rclunk
	}

	csend(&cf)

	switch f.Type {
	case plan9.Rclunk:
		fallthrough
	case plan9.Rremove:
		if !r.flushing {
			freefid(r.fid)
		}
	case plan9.Rwalk:
		if !r.flushing {
			if len(r.fcall.Wname) != len(f.Wqid) { // Walk did not succeed, leave everything unaffected
				freefid(r.newfid)
				r.newfid = nil
				break
			}
			nwq := len(f.Wqid)
			wq := f.Wqid
			if r.newfid != nil { // TODO: this does not work, even in the C version
				clonefid(r.fid, r.newfid)
				// TODO: check with original: original SHOULD fail with out of bounds, for len(f.Wqid)==0
				if nwq > 0 {
					r.newfid.fidtype = wq[nwq-1].Type // TODO, understand what's this
				} else {
					chat("len(f.Wqid): %v==0", len(f.Wqid)) // TODO: redo nwq==0?
					r.newfid.fidtype = 0                    // fgergo: QTFILE?
				}
			} else {
				r.fid.fidtype = wq[nwq-1].Type // TODO: redo nwq==0?
			}
			for i := 0; i < len(f.Wqid); i++ {
				r.newfid.cname = namewalk(r.newfid.cname, r.fcall.Wname[i])
				r.newfid.sname = namewalk(r.newfid.sname, r.fcall.Wname[i])
			}
		}

	case plan9.Rcreate:
		fid := r.fid
		if !r.flushing {
			fid.cname = namewalk(fid.cname, r.fcall.Name)
			fid.sname = namewalk(fid.sname, r.fcall.Name)
			fid.isopen = true
			fid.omode = r.fcall.Mode
		}

	case plan9.Ropen:
		fid := r.fid
		if !r.flushing {
			fid.isopen = true
			fid.omode = r.fcall.Mode
		}

	case plan9.Rflush:
		/*
		 *	If we sent a flush for them,
		 *	we have to free the frozen fids now
		 */
		flushr := lookuprreq(uint16(r.fcall.Oldtag))
		if flushr != nil {
			switch flushr.fcall.Type {
			case plan9.Tremove:
				fallthrough
			case plan9.Tattach:
				if flushr.fid != nil {
					freefid(flushr.fid)
				}
			case plan9.Twalk:
				if flushr.newfid != nil && flushr.newfid != flushr.fid {
					freefid(flushr.newfid)
				}
			}
			freereq(flushr)
		}

	case plan9.Rerror:
		switch r.fcall.Type {
		case plan9.Twalk:
			if r.newfid != nil {
				freefid(r.newfid)
				r.newfid = nil
			}
		case plan9.Tclunk: //  can't happen
			must(false, "case plan9.Tclunk can't happen")
			// fallthrough
		case plan9.Tremove:
			freefid(r.fid)
		}
	}

	/*
	 * If we're waiting for an Rflush to come back for that message,
	 * we can't free the request until it comes back (else we'll reuse
	 * the tag, and get confused when the Rflush arrives).
	 */
	if !r.flushing {
		freereq(r)
	}
}

func clientrecover(r *Req, f *Fid) {
	chat("clientrecover(), recovering[%v], remote  path is: %s, for fcall %v", f.fid.remote, f.cname, &r.fcall)
	if r.fid.fid.remote == f.fid.remote {
		queuereq(r)
	}
}

func clienterror(r *Req, f *Fid) {
	var nr *Req
	var fc plan9.Fcall

	if r.fid == f {
		/*
		 * If we have an outstanding Tclunk,
		 * we need to send back an Rclunk rather than an Rerror,
		 * and we have to clunk the fid on the remote server.
		 *
		 * If we have an outstanding Tremove,
		 * we need to clunk the fid on the remote server.
		 */
		switch r.fcall.Type {
		case plan9.Tclunk:
			fc.Type = plan9.Rclunk
			fc.Tag = uint16(r.tag.local)
			fc.Fid = uint32(f.fid.local)
			csend(&fc)
			fallthrough
		case plan9.Tremove:
			nr = allocreq(taggen)
			taggen++
			nr.fid = f
			nr.fcall.Type = plan9.Tclunk
			nr.internal = true
			queuereq(nr)
			if r.fcall.Type == plan9.Tclunk {
				break
			}
			fallthrough
		default:
			cerror(uint16(r.tag.local), "couldn't recover fid after lost connection")
		}
		freereq(r)
	}
}

func internalresponse(r *Req, f *plan9.Fcall) {
	switch f.Type {
	default:
		chat("unexpected internal response %v %v", *f, r.fcall)
		dumplast()
		freereq(r)

	case plan9.Rread:
		freereq(r)

	case plan9.Rclunk:
		freereq(r)

	case plan9.Rattach:
		fid := r.fid
		a := fid.attach

		a.gen = gen
		a.rootqid = f.Qid
		freereq(r)
		fid.gen = gen
		forallreqs(clientrecover, fid)

	case plan9.Rwalk:
		if len(r.fcall.Wname) != len(f.Wqid) { //Walk did not succeed, leave everything unaffected
			freefid(r.newfid)
			r.newfid = nil
			break
		}

		if r.flushing {
			break
		}

		wq := f.Wqid
		nwq := len(f.Wqid)

		var fid *Fid
		var isdir bool

		if r.newfid != nil {
			clonefid(r.fid, r.newfid)
			r.newfid.fidtype = wq[nwq-1].Type // TODO, later, review nwq==0 ?
			isdir = (r.newfid.fidtype & plan9.QTDIR) == plan9.QTDIR
			fid = r.newfid
		} else {
			r.fid.fidtype = wq[nwq-1].Type
			isdir = (r.fid.fidtype & plan9.QTDIR) == plan9.QTDIR
			fid = r.fid
		}
		for i := 0; i < len(f.Wqid); i++ {
			fid.cname = namewalk(fid.cname, r.fcall.Wname[i])
			fid.sname = namewalk(fid.sname, r.fcall.Wname[i])
		}
		if fid.sname != fid.cname {
			r.fcall.Type = plan9.Twalk
			r.fid = fid
			r.fcall.Wname[0] = nextelem(fid.sname, fid.cname)
			queuereq(r)
			break
		} else if fid.isopen {
			r.fcall.Type = plan9.Topen
			r.fcall.Mode = fid.omode
			r.fid = fid
			if isdir {
				r.fid.dirccount = r.fid.dirscount //paranoid
				r.fid.dirscount = 0
			}
			queuereq(r)
			break
		}

		// next 4 lines were copied from case plan9.Ropen block Recover: label
		fid.gen = gen
		chat("case Rwalk: reestablished fid: %v", fid)
		freereq(r)
		forallreqs(clientrecover, fid)

	case plan9.Ropen:
		fid := r.fid
		fid.fidtype = r.fcall.Qid.Type

		// fall through
		//	Recover:
		fid.gen = gen
		chat("case Ropen: reestablished fid: %v", fid)
		freereq(r)
		forallreqs(clientrecover, fid)

	case plan9.Rerror:
		fid := r.fid
		fid.gen = gen
		freereq(r)
		forallreqs(clienterror, fid)
	}
}

func listennet() {
	for {
		for {
			f, err := plan9.ReadFcall(netconn)
			if err != nil {
				break
			}

			thelock.Lock()

			last[nlast&(len(last)-1)] = *f
			nlast++

			chat("net -> %v", f)

			r := lookuprreq(f.Tag)
			if r == nil {
				chat("netconn: unexpected fcall: %v", f)
				dumplast()

				forallreqs(dumpreq, nil)
				thelock.Unlock()
				continue
			}
			if f.Type != r.fcall.Type+1 && f.Type != plan9.Rerror {
				chat("netconn, mismatch: %v, got: %v", r.fcall, f)
				thelock.Unlock()
				continue
			}

			if r.fcall.Type == plan9.Ropen {
				r.fid.vers = r.fcall.Qid.Vers
				r.fid.fidtype = r.fcall.Qid.Type
			}
			if r.internal {
				internalresponse(r, f)
			} else {
				externalresponse(r, f)
			}

			thelock.Unlock()
		}

		// wait for requests to come in
		nreq := forallreqs(func(r *Req, f *Fid) {}, nil)
		for nreq == 0 {
			thelock.Lock()
			nreq = forallreqs(func(r *Req, f *Fid) {}, nil)
			thelock.Unlock()
			time.Sleep(time.Second)
		}

		thelock.Lock()
		redial()
		thelock.Unlock()
	}
}
