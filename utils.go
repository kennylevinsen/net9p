package net9p

import (
	"github.com/joushou/qp"
	"github.com/joushou/qptools/fileserver/trees"
)

type openCounterHandle struct {
	trees.ReadWriteAtCloser
	closeHook func()
}

func (h *openCounterHandle) Close() error {
	err := h.ReadWriteAtCloser.Close()
	h.closeHook()
	return err
}

type openCounterFile struct {
	trees.File
	OpenHook  func()
	CloseHook func()
}

func (f *openCounterFile) Open(user string, mode qp.OpenMode) (trees.ReadWriteAtCloser, error) {
	res, err := f.File.Open(user, mode)
	if err != nil {
		return res, err
	}

	f.OpenHook()
	return &openCounterHandle{
		ReadWriteAtCloser: res,
		closeHook:         f.CloseHook,
	}, nil
}

func newOpenCounterFile(f trees.File, open func(), close func()) trees.File {
	return &openCounterFile{
		File:      f,
		OpenHook:  open,
		CloseHook: close,
	}
}

type MagicWalkFile struct {
	*trees.SyntheticFile
	MagicWalkHook func(user string) (trees.File, error)
}

func (f *MagicWalkFile) Arrived(user string) (trees.File, error) {
	if f.MagicWalkHook == nil {
		return nil, nil
	}
	return f.MagicWalkHook(user)
}

func NewMagicWalkFile(name string, permissions qp.FileMode, user, group string, hook func(user string) (trees.File, error)) *MagicWalkFile {
	return &MagicWalkFile{
		SyntheticFile: trees.NewSyntheticFile(name, permissions, user, group),
		MagicWalkHook: hook,
	}
}
