package net9p

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joushou/qp"
	"github.com/joushou/qptools/fileserver/trees"
)

const (
	stateEstablished = "Established"
	stateAnnouncing  = "Announcing"
	stateClosed      = "Closed"
)

type conn struct {
	sync.Mutex
	conn           net.Conn
	listener       net.Listener
	network        string
	address        string
	connectPending bool
	owned          uint32
	state          string
}

func (c *conn) getState() string {
	c.Lock()
	defer c.Unlock()
	return c.state
}

func (c *conn) command(s string) error {
	c.Lock()
	defer c.Unlock()
	s = strings.TrimSpace(s)

	args := strings.Split(s, " ")
	cmd := args[0]
	args = args[1:]
	switch cmd {
	case "connect":
		addr := strings.Split(args[0], "!")
		if len(args) == 2 {
			return errors.New("manual local port assignment not supported")
		}
		if len(addr) != 2 {
			return errors.New("invalid address")
		}
		host := addr[0]
		port, err := strconv.ParseUint(addr[1], 10, 64)
		if err != nil {
			return err
		}
		if port > 65535 {
			return fmt.Errorf("invalid port: %s", addr[1])
		}

		// TODO(kl): Should the old connection disconnect?
		c.address = fmt.Sprintf("%s:%d", host, port)
		log.Printf("-> Connect: %s", c.address)
		c.connectPending = true

	case "announce":
		if args[0] == "*" {
			args[0] = "0"
		}

		port, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		if port > 65535 {
			return fmt.Errorf("invalid port: %s", args[0])
		}

		c.address = fmt.Sprintf(":%d", port)
		log.Printf("-> Announce: %s", c.address)
		c.listener, err = net.Listen(c.network, c.address)
		c.state = stateAnnouncing

	case "accept":
		// Do nothing...
	case "keepalive":
		var tcp *net.TCPConn
		var ok bool

		if tcp, ok = c.conn.(*net.TCPConn); !ok {
			return fmt.Errorf("keepalive only valid on tcp connections")
		}
		var period time.Duration
		switch len(args) {
		case 0:
			period = 30 * time.Second
		case 1:
			d, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			period = time.Duration(d) * time.Millisecond
		default:
			return fmt.Errorf("invalid arguments")
		}

		log.Printf("-> Keepalive: %v", period)
		tcp.SetKeepAlivePeriod(period)
		tcp.SetKeepAlive(true)

	case "hangup", "reject":
		c.hangup()
	case "bind", "ttl", "tos", "ignoreadvice", "addmulti", "remmulti":
		return fmt.Errorf("Unimplemented command received: %v", s)
	case "checksum", "tcpporthogdefence":
		return fmt.Errorf("Unimplemented command received: %v", s)
	default:
		return fmt.Errorf("Unimplemented command received: %v", s)
	}

	return nil
}

func (c *conn) dial() error {
	c.Lock()
	defer c.Unlock()
	var err error
	if c.connectPending {
		c.connectPending = false
		c.conn, err = net.Dial(c.network, c.address)
		if err != nil {
			return err
		}
		log.Printf("-> Connected: %v", c.conn.RemoteAddr())
		c.state = stateEstablished
	}
	if c.conn == nil {
		return errors.New("not connected")
	}
	return err
}

func (c *conn) listen() (net.Conn, error) {
	c.Lock()
	defer c.Unlock()

	if c.listener == nil {
		return nil, errors.New("not announced")
	}
	nc, err := c.listener.Accept()
	if err != nil {
		return nil, err
	}

	log.Printf("-> Accept: %v", nc.RemoteAddr())
	return nc, nil
}

func (c *conn) hangup() {
	c.Lock()
	defer c.Unlock()
	if c.conn != nil {
		log.Printf("-> Hangup %v", c.conn.RemoteAddr())
		c.conn.Close()
		c.state = stateClosed
	}
}

func (c *conn) own() {
	c.Lock()
	c.owned++
	c.Unlock()
}

func (c *conn) disown() {
	c.Lock()
	c.owned--
	shouldHangup := c.owned == 0
	c.Unlock()

	if shouldHangup {
		c.hangup()
	}
}

type dataFile struct {
	sync.RWMutex
	*trees.SyntheticFile
	dead  uint32
	mconn *conn
}

func (f *dataFile) Open(user string, mode qp.OpenMode) (trees.ReadWriteAtCloser, error) {
	f.Lock()
	defer f.Unlock()
	if !f.CanOpen(user, mode) {
		return nil, errors.New("permission denied")
	}

	if err := f.mconn.dial(); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *dataFile) ReadAt(p []byte, _ int64) (int, error) {
	x := atomic.LoadUint32(&f.dead)
	if x > 0 {
		return 0, errors.New("connection closed")
	}

	n, err := f.mconn.conn.Read(p)

	if err == io.EOF {
		atomic.AddUint32(&f.dead, 1)
	}
	return n, err
}

func (f *dataFile) WriteAt(p []byte, _ int64) (int, error) {
	x := atomic.LoadUint32(&f.dead)
	if x > 0 {
		return 0, errors.New("connection closed")
	}

	n, err := f.mconn.conn.Write(p)

	if err == io.EOF {
		atomic.AddUint32(&f.dead, 1)
	}
	return n, err
}

func (f *dataFile) Close() error {
	return nil
}

func newDataFile(name string, permissions qp.FileMode, user, group string, conn *conn) *dataFile {
	return &dataFile{
		SyntheticFile: trees.NewSyntheticFile(name, permissions, user, group),
		mconn:         conn,
	}
}

type ctlFile struct {
	*trees.SyntheticFile
	buf     []byte
	buflock sync.Mutex

	mconn *conn
}

func (f *ctlFile) Open(user string, mode qp.OpenMode) (trees.ReadWriteAtCloser, error) {
	if !f.CanOpen(user, mode) {
		return nil, errors.New("permission denied")
	}
	return f, nil
}

func (f *ctlFile) ReadAt(p []byte, _ int64) (int, error) {
	f.buflock.Lock()
	defer f.buflock.Unlock()

	n := len(f.buf)
	if n > len(p) {
		n = len(p)
	}
	copy(p, f.buf)
	f.buf = f.buf[:n]

	return n, nil
}

func (f *ctlFile) response(b []byte) {
	f.buflock.Lock()
	defer f.buflock.Unlock()
	f.buf = append(f.buf, b...)
}

func (f *ctlFile) WriteAt(p []byte, _ int64) (int, error) {
	return len(p), f.mconn.command(string(p))
}

func (f *ctlFile) Close() error {
	return nil
}

func newCtlFile(name string, permissions qp.FileMode, user, group string, conn *conn) *ctlFile {
	return &ctlFile{
		SyntheticFile: trees.NewSyntheticFile(name, permissions, user, group),
		mconn:         conn,
	}
}
