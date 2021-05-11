package main

import (
	"9fans.net/go/plan9"
	"errors"
	"fmt"
)

var currmsize uint32

/*
 * Initial connection setup.  Assumes this is the only traffic
 * on the connection, so no need to multiplex or worry about
 * threads.
 */

/*
 * run 9P transaction; if errok is true Rerror is expected, hence not an error
 * transaction() sends on netconn a 9p message in req and returns reply
 */
func transaction(req *plan9.Fcall, errok bool) (*plan9.Fcall, error) {
	err := plan9.WriteFcall(netconn, req)
	if err != nil {
		logerror("transaction WriteFcall() error, err: %v", err)
		return nil, err
	}
	chat("net <- %v", req)

	resp, err := plan9.ReadFcall(netconn)
	if err != nil {
		logerror("transaction Read() error, err: %v", err)
		return nil, err
	}
	chat("net -> %v", resp)

	if resp.Tag != req.Tag {
		err = errors.New(fmt.Sprintf("unexpected resp.Tag %v != req.Tag: %v", resp.Tag, req.Tag))
		logerror("transaction err: %v", err)
		return nil, err
	}
	if resp.Type == plan9.Rerror && !errok {
		err = errors.New(fmt.Sprintf("transaction response: %v", resp.Ename))
		logerror("transaction err: %v", err)
		return nil, err
	}
	if resp.Type != plan9.Rerror && resp.Type != req.Type+1 {
		err = errors.New(fmt.Sprintf("transaction response: unexpected resp.Type %v", resp.Type))
		logerror("transaction err: %v", err)
		return nil, err
	}

	return resp, nil
}

func xversion() error {
	var f plan9.Fcall

	f.Type = plan9.Tversion
	f.Tag = plan9.NOTAG
	f.Msize = MAXPKT
	f.Version = "9P2000"

	must(netconn != nil, "netconn == nil")
	r, err := transaction(&f, false)
	if err != nil {
		return err
	}

	if f.Msize > MAXPKT {
		logfatal("server msize %v > requested msize %v", r.Msize, MAXPKT)
	}

	if currmsize == 0 {
		currmsize = f.Msize // initialize currmsize
	}

	if currmsize > r.Msize {
		logfatal("server reduced msize on reconnect - was %v, now %v", currmsize, r.Msize)
	}

	if r.Version != "9P2000" {
		logfatal("server wants to speak %v", r.Version)
	}

	return nil
}

func xclunk(fid *Fid) (*plan9.Fcall, error) {
	var f plan9.Fcall

	f.Type = plan9.Tclunk
	f.Fid = fid.fid.remote

	return transaction(&f, false)
}

func xread(fid *Fid, n int) ([]byte, error) {
	var req plan9.Fcall

	req.Type = plan9.Tread
	req.Fid = fid.fid.remote
	req.Count = uint32(n)

	resp, err := transaction(&req, false)
	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

func xwrite(fid *Fid, buf []byte) (int, error) {
	var req plan9.Fcall

	req.Type = plan9.Twrite
	req.Fid = fid.fid.remote
	req.Count = uint32(len(buf))
	req.Data = buf

	resp, err := transaction(&req, false)
	if err != nil {
		return 0, err
	}

	return int(resp.Count), nil
}

func xauth(fid *Fid, uname string, aname string) (int, error) {
	var req plan9.Fcall

	req.Type = plan9.Tauth
	req.Afid = fid.fid.remote
	req.Uname = uname
	req.Aname = aname

	resp, err := transaction(&req, true)
	if err != nil {
		return -1, err
	}

	if resp.Type == plan9.Rerror {
		return 0, nil
	}

	return 1, nil
}

/*
enum {
	ARgiveup = 100,
}


static int
dorpc(AuthRpc *rpc, char *verb, char *val, int len, AuthGetkey *getkey)
{
	int ret;

	for(;;){
		if((ret = auth_rpc(rpc, verb, val, len)) != ARneedkey && ret != ARbadkey)
			return ret;
		if(getkey == nil)
			return ARgiveup;	// don't know how
		if((*getkey)(rpc->arg) < 0)
			return ARgiveup;	// user punted
	}
}
*/

/*
 *  this just proxies what the factotum tells it to.
static AuthInfo*
xfauthproxy(Fid *afid, AuthRpc *rpc, AuthGetkey *getkey, char *params)
{
	char *buf;
	int m, n, ret;
	AuthInfo *a;
	char oerr[ERRMAX];

	rerrstr(oerr, sizeof oerr);
	werrstr("UNKNOWN AUTH ERROR");

	if(dorpc(rpc, "start", params, strlen(params), getkey) != ARok){
		werrstr("xfauth_proxy start: %r");
		return nil;
	}

	buf = malloc(AuthRpcMax);
	if(buf == nil)
		return nil;
	for(;;){
		switch(dorpc(rpc, "read", nil, 0, getkey)){
		case ARdone:
			free(buf);
			a = auth_getinfo(rpc);
			errstr(oerr, sizeof oerr);	// no error, restore whatever was there
			return a;
		case ARok:
			if(xwrite(afid, rpc->arg, rpc->narg) != rpc->narg){
				werrstr("auth_proxy write fd: %r");
				goto Error;
			}
			break;
		case ARphase:
			n = 0;
			memset(buf, 0, AuthRpcMax);
			while((ret = dorpc(rpc, "write", buf, n, getkey)) == ARtoosmall){
				if(atoi(rpc->arg) > AuthRpcMax)
					break;
				m = xread(afid, buf+n, atoi(rpc->arg)-n);
				if(m <= 0){
					if(m == 0)
						werrstr("auth_proxy short read: %s", buf);
					goto Error;
				}
				n += m;
			}
			if(ret != ARok){
				werrstr("auth_proxy rpc write: %s: %r", buf);
				goto Error;
			}
			break;
		default:
			werrstr("auth_proxy rpc: %r");
			goto Error;
		}
	}
Error:
	free(buf);
	return nil;
}

static AuthInfo*
xauthproxy(Fid *authfid, AuthGetkey *getkey, char *fmt, ...)
{

	char *p;
	va_list arg;
	AuthInfo *ai;
	AuthRpc *rpc;

	va_start(arg, fmt);
	p = vsmprint(fmt, arg);
	va_end(arg);

	if((rpc = auth_allocrpc_wrap()) == nil){
		free(p);
		return nil;
	}

	ai = xfauthproxy(authfid, rpc, getkey, p);
	free(p);
	auth_freerpc(rpc);
	return ai;
}
*/

func xattach(fid *Fid, afid *Fid, uname string, aname string) (plan9.Qid, error) {
	var req plan9.Fcall

	req.Type = plan9.Tattach
	req.Fid = fid.fid.remote
	req.Afid = plan9.NOFID
	if afid != nil {
		req.Afid = afid.fid.remote
	}
	req.Uname = uname
	req.Aname = aname

	resp, err := transaction(&req, false)
	if err != nil {
		return resp.Qid, err // should be nil, err
	}

	return resp.Qid, nil
}

func authattach(a *Attach) int {
	authfid := allocfid(fidgen)
	fidgen++

	authresult, err := xauth(authfid, eve, a.aname)
	switch authresult {
	case -1:
		return -1
	case 0:
		freefid(authfid)
		authfid = nil
	case 1:
		// not implemented in go version
		return -1
	}

	chat("authattach(), attach: %v", a)
	a.rootqid, err = xattach(a.rootfid, authfid, eve, a.aname)
	if err != nil {
		logerror("xattach() error: %v", err)
		if authfid != nil {
			xclunk(authfid)
			freefid(authfid)
		}
		return -1
	}
	chat("xattach(), returned ok")

	if authfid != nil {
		xclunk(authfid)
		freefid(authfid)
	}

	return 0
}
