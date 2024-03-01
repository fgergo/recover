package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/user"
	"strings"
	"sync"

	"github.com/fgergo/p9plib"
)

var (
	netconn net.Conn            // 9p server connection to be kept alive
	srvconn p9plib.Stdio9pserve // clients of recover connect here

	gen        int
	attachlist *Attach

	eve string = "eve"

	thelock sync.Mutex

	exitch chan bool

	flag_chatty = flag.Bool("d", false, "display verbose messages")

	dialstring string
	srvname    string
	spec       string
)

func main() {
	flag.Usage = func() {
		fmt.Printf("Usage: %s [ -d ] [ net!]host [ srvname ] [ spec ]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	switch len(args) {
	case 0:
		flag.Usage()
		os.Exit(1)
	case 3:
		spec = args[2]
		srvname = args[1] + spec
	case 2:
		srvname = args[1]
	case 1:
		srvname = strings.ReplaceAll(args[0], ":", "_") // windows can't create filename with colon
	}
	dialstring = args[0]

	fids.init()
	reqs.init()

	u, err := user.Current()
	if err != nil {
		sysfatal("user.Current, err: %v", err)
	} else {
		eve = u.Username
	}
	err = p9plib.Post9pservice(&srvconn, srvname)
	if err != nil {
		sysfatal("Post9pservice(), error: %v", err)
	}
	attachment(spec)
	redial()

	exitch = make(chan bool)
	go listennet()
	go listensrv()
	<-exitch
}
