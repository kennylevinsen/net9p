package net9p

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/kennylevinsen/qp"
	"github.com/kennylevinsen/qptools/fileserver/trees"
)

type csHandle struct {
	buf     [][]byte
	buflock sync.Mutex
}

func (h *csHandle) ReadAt(p []byte, _ int64) (int, error) {
	h.buflock.Lock()
	defer h.buflock.Unlock()

	if len(h.buf) == 0 {
		return 0, nil
	}

	b := h.buf[0]
	copy(p, b)
	h.buf = h.buf[1:]
	return len(b), nil
}

func (h *csHandle) WriteAt(p []byte, _ int64) (int, error) {
	h.buflock.Lock()
	defer h.buflock.Unlock()

	cmd := strings.Split(strings.TrimSpace(string(p)), "!")

	switch len(cmd) {
	default:
		return len(p), errors.New("invalid query")
	case 2:
		h.buf = append(h.buf, []byte(fmt.Sprintf("/net/%s/clone %s", cmd[0], cmd[1])))
		return len(p), nil
	case 3:
		break
	}

	if cmd[0] == "net" {
		// Whatever
		cmd[0] = "tcp"
	}

	switch cmd[2] {
	case "9fs", "9pfs":
		cmd[2] = "564"
	}

	port, err := net.LookupPort(cmd[0], cmd[2])
	if err != nil {
		return len(p), err
	}

	var addrs []string
	if cmd[1] == "*" {
		addrs = []string{""}
	} else {
		addrs, err = net.LookupHost(cmd[1])
		if err != nil {
			return len(p), err
		}
	}

	for _, addr := range addrs {
		var a string
		if addr == "" {
			a = fmt.Sprintf("%d", port)
		} else {
			a = fmt.Sprintf("%s!%d", addr, port)
		}
		h.buf = append(h.buf, []byte(fmt.Sprintf("/net/%s/clone %s", cmd[0], a)))
	}

	return len(p), err
}

func (h *csHandle) Close() error {
	return nil
}

type csFile struct {
	*trees.SyntheticFile
}

func (f *csFile) Open(user string, mode qp.OpenMode) (trees.ReadWriteAtCloser, error) {
	if !f.CanOpen(user, mode) {
		return nil, errors.New("access denied")
	}
	return &csHandle{}, nil
}

func NewCSFile(user, group string) trees.File {
	return &csFile{
		SyntheticFile: trees.NewSyntheticFile("cs", 0777, user, group),
	}
}
