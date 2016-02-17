package net9p

/*

/
   net/
      tcp/
         clone
         stats
         0/
            ctl
            data
            listen # (let's start without)
            local
            remote
            status

*/

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/joushou/qptools/fileserver/trees"
)

type TCPUDPHandler struct {
	sync.Mutex
	root        *trees.SyntheticDir
	network     string
	user, group string
	cnt         uint16
}

func (h *TCPUDPHandler) new(c net.Conn) (trees.File, error) {
	curcnt := h.cnt
	h.cnt++

	conn := &conn{state: "Closed", network: h.network, conn: c}
	if c != nil {
		conn.state = "Established"
	}

	dir := trees.NewSyntheticDir(fmt.Sprintf("%d", curcnt), 0777, h.user, h.group)

	h.root.Add(fmt.Sprintf("%d", curcnt), dir)

	ctlfile := newCtlFile("ctl", 0777, h.user, h.group, conn)
	ctlfile.response([]byte(fmt.Sprintf("%d", curcnt)))
	dir.Add("ctl", newOpenCounterFile(ctlfile, conn.own, conn.disown))

	datafile := newDataFile("data", 0777, h.user, h.group, conn)
	dir.Add("data", newOpenCounterFile(datafile, conn.own, conn.disown))

	localfile := NewMagicWalkFile("local", 0555, h.user, h.group, func(string) (trees.File, error) {
		// BUG(kl): Refresh requires walking to the file!
		var cnt []byte
		if conn.conn != nil {
			y := conn.conn.LocalAddr().String()
			cnt = []byte(strings.Replace(y, ":", "!", 1))
		}
		x := trees.NewSyntheticFile("local", 0555, h.user, h.group)
		x.SetContent(cnt)
		return newOpenCounterFile(x, conn.own, conn.disown), nil
	})
	dir.Add("local", localfile)

	remotefile := NewMagicWalkFile("remote", 0555, h.user, h.group, func(string) (trees.File, error) {
		// BUG(kl): Refresh requires walking to the file!
		var cnt []byte
		if conn.conn != nil {
			y := conn.conn.RemoteAddr().String()
			cnt = []byte(strings.Replace(y, ":", "!", 1))
		}
		x := trees.NewSyntheticFile("remote", 0555, h.user, h.group)
		x.SetContent(cnt)
		return newOpenCounterFile(x, conn.own, conn.disown), nil
	})
	dir.Add("remote", remotefile)

	statusfile := NewMagicWalkFile("status", 0555, h.user, h.group, func(string) (trees.File, error) {
		// BUG(kl): Refresh requires walking to the file!
		x := trees.NewSyntheticFile("status", 0555, h.user, h.group)
		x.SetContent([]byte(fmt.Sprintf("%s\n", conn.getState())))
		return newOpenCounterFile(x, conn.own, conn.disown), nil
	})
	dir.Add("status", statusfile)

	listenfile := NewMagicWalkFile("listen", 0777, h.user, h.group, func(string) (trees.File, error) {
		c, err := conn.listen()
		if err != nil {
			return nil, err
		}
		return h.new(c)
	})
	dir.Add("listen", listenfile)

	return ctlfile, nil
}

func (h *TCPUDPHandler) next() (trees.File, error) {
	h.Lock()
	defer h.Unlock()

	return h.new(nil)
}

func NewTCPDir(user, group string) trees.Dir {
	dir := trees.NewSyntheticDir("tcp", 0777, user, group)

	tcphandler := &TCPUDPHandler{
		root:    dir,
		network: "tcp",
	}

	clonefile := NewMagicWalkFile("clone", 0777, user, group, func(user string) (trees.File, error) {
		return tcphandler.next()
	})
	dir.Add("clone", clonefile)

	return dir
}

func NewUDPDir(user, group string) trees.Dir {
	dir := trees.NewSyntheticDir("udp", 0777, user, group)

	udphandler := &TCPUDPHandler{
		root:    dir,
		network: "udp",
	}

	clonefile := NewMagicWalkFile("clone", 0777, user, group, func(user string) (trees.File, error) {
		return udphandler.next()
	})
	dir.Add("clone", clonefile)

	return dir
}
