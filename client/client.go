package client

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/joushou/qp"
	"github.com/joushou/qptools/client"
)

var ErrNotAvailable = errors.New("not available")

type IPConnAddr struct {
	network string
	address string
}

func (ipca IPConnAddr) Network() string {
	return ipca.network
}

func (ipca IPConnAddr) String() string {
	return ipca.address
}

type IPConn struct {
	sync.RWMutex
	rootfid client.Fid
	ctlfid  client.Fid
	datafid client.Fid

	localAddr  IPConnAddr
	remoteAddr IPConnAddr
}

func (ipc *IPConn) LocalAddr() net.Addr {
	return ipc.localAddr
}

func (ipc *IPConn) RemoteAddr() net.Addr {
	return ipc.remoteAddr
}

func (ipc *IPConn) SetDeadline(t time.Time) error {
	return ErrNotAvailable
}

func (ipc *IPConn) SetReadDeadline(t time.Time) error {
	return ErrNotAvailable
}

func (ipc *IPConn) SetWriteDeadline(t time.Time) error {
	return ErrNotAvailable
}

func (ipc *IPConn) openData() error {
	var err error
	if ipc.datafid, _, err = ipc.rootfid.Walk([]string{"data"}); err != nil {
		return err
	}

	if _, _, err = ipc.datafid.Open(qp.ORDWR); err != nil {
		return err
	}

	return nil
}

func (ipc *IPConn) dial(addr string) error {
	ipc.Lock()
	defer ipc.Unlock()
	var err error
	if _, err = ipc.ctlfid.WriteOnce(0, []byte("connect "+addr)); err != nil {
		return err
	}

	return ipc.openData()
}

func (ipc *IPConn) Read(p []byte) (int, error) {
	ipc.RLock()
	defer ipc.RUnlock()
	max := uint32(len(p))
	b, err := ipc.datafid.ReadOnce(0, max)
	copy(p, b)
	return len(b), err
}

func (ipc *IPConn) Write(p []byte) (int, error) {
	ipc.RLock()
	defer ipc.RUnlock()
	return client.WrapFid(ipc.datafid).WriteAt(p, 0)
}

func (ipc *IPConn) Close() error {
	ipc.Lock()
	defer ipc.Unlock()
	if ipc.rootfid != nil {
		ipc.rootfid.Clunk()
		ipc.rootfid = nil
	}

	if ipc.ctlfid != nil {
		ipc.ctlfid.Clunk()
		ipc.ctlfid = nil
	}

	if ipc.datafid != nil {
		ipc.datafid.Clunk()
		ipc.datafid = nil
	}

	return nil
}

type IPListener struct {
	rootfid client.Fid
}

func (ipl *IPListener) Addr() net.Addr {
	return nil
}

func (ipl *IPListener) Accept() (net.Conn, error) {
	// Should we *walk* to it?
	f, _, err := ipl.rootfid.Walk([]string{"listen"})
	if err != nil {
		return nil, fmt.Errorf("unable to go to listen file: %v", err)
	}

	if _, _, err := f.Open(qp.ORDWR); err != nil {
		return nil, fmt.Errorf("unable to open ctl file: %v", err)
	}

	b, err := f.ReadOnce(0, 1024)
	if err != nil {
		return nil, fmt.Errorf("unable to read conn path: %v", err)
	}

	rf, _, err := ipl.rootfid.Walk([]string{"..", string(b)})
	if err != nil {
		return nil, fmt.Errorf("unable to go to conn %s: %v", b, err)
	}

	ipc := &IPConn{
		rootfid: rf,
		ctlfid:  f,
	}

	return ipc, ipc.openData()
}

func (ipl *IPListener) Close() error {
	return nil
}

type IP struct {
	fid client.Fid
}

func (ip *IP) lookup(query string) ([]string, error) {
	f, _, err := ip.fid.Walk([]string{"cs"})
	if err != nil {
		return nil, err
	}
	defer f.Clunk()

	if _, _, err := f.Open(qp.ORDWR); err != nil {
		return nil, err
	}

	if _, err := f.WriteOnce(0, []byte(query)); err != nil {
		return nil, err
	}

	off := uint64(0)

	var res []string
	for {
		b, err := f.ReadOnce(off, 1024)
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			break
		}
		off += uint64(len(b))
		res = append(res, string(b))
	}

	return res, nil
}

func getConn(ctlfid, rootfid client.Fid, network string) (client.Fid, error) {
	if _, _, err := ctlfid.Open(qp.ORDWR); err != nil {
		return nil, fmt.Errorf("unable to open ctl file: %v", err)
	}

	b, err := ctlfid.ReadOnce(0, 1024)
	if err != nil {
		return nil, fmt.Errorf("unable to read conn path: %v", err)
	}

	rf, _, err := rootfid.Walk([]string{network, string(b)})
	if err != nil {
		return nil, fmt.Errorf("unable to go to conn %s: %v", network+"/"+string(b), err)
	}

	return rf, nil
}

func (ip *IP) clone(network, address string) (client.Fid, string, string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, "", "", fmt.Errorf("unable to parse address: %v", err)
	}

	res, err := ip.lookup(fmt.Sprintf("%s!%s!%s", network, host, port))
	if err != nil {
		return nil, "", "", fmt.Errorf("lookup failed: %v", err)
	}

	var addr string
	var path []string
	if len(res) == 0 {
		addr = fmt.Sprintf("%s!%s", host, port)
		path = []string{network, "clone"}
	} else {
		x := strings.Split(res[0], " ")
		if len(x) != 2 {
			return nil, "", "", fmt.Errorf("unexpected lookup response: %s", res[0])
		}

		path = strings.Split(x[0], "/")
		if len(path) != 4 {
			return nil, "", "", fmt.Errorf("unexpected dial path: %s", x[0])
		}

		network = path[2]
		path = path[2:]
		addr = x[1]
	}

	f, _, err := ip.fid.Walk(path)
	if err != nil {
		return nil, network, addr, fmt.Errorf("unable to walk to ctl file: %v", err)
	}

	return f, network, addr, nil
}

func (ip *IP) Dial(network, address string) (net.Conn, error) {
	f, _, addr, err := ip.clone(network, address)
	if err != nil {
		return nil, err
	}

	rf, err := getConn(f, ip.fid, network)
	if err != nil {
		return nil, err
	}

	ipc := &IPConn{
		rootfid: rf,
		ctlfid:  f,
	}

	ipc.dial(addr)

	return ipc, nil
}

func (ip *IP) Listen(network, address string) (net.Listener, error) {
	f, _, addr, err := ip.clone(network, address)
	if err != nil {
		return nil, err
	}
	s := strings.Split(addr, "!")
	if len(s) != 2 {
		return nil, errors.New("unknown parse error")
	}

	rf, err := getConn(f, ip.fid, network)
	if err != nil {
		return nil, err
	}

	if _, err := f.WriteOnce(0, []byte(fmt.Sprintf("announce %s", s[1]))); err != nil {
		return nil, err
	}

	ipl := &IPListener{
		rootfid: rf,
	}

	return ipl, nil
}

func NewIP(fid client.Fid) *IP {
	return &IP{fid: fid}
}
