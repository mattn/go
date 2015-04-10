// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package net

import (
	"internal/poll"
	"internal/syscall/windows"
	"os"
	"syscall"
)

func dupCloseOnExec(s int) (syscall.Handle, error) {
	var ns syscall.Handle
	p, _ := syscall.GetCurrentProcess()
	err := syscall.DuplicateHandle(p, syscall.Handle(s), p, &ns, 0, false, syscall.DUPLICATE_SAME_ACCESS)
	if err != nil {
		return syscall.InvalidHandle, os.NewSyscallError("duplicatehandle", err)
	}
	return ns, nil
}

func newFileFD(f *os.File) (*netFD, error) {
	var flags uint32

	var err error
	s := syscall.Handle(f.Fd())
	if err := windows.GetHandleInformation(s, &flags); err != nil {
		return nil, err
	}

	s, err = dupCloseOnExec(int(f.Fd()))
	if err != nil {
		return nil, err
	}

	if err = syscall.SetNonblock(s, true); err != nil {
		poll.CloseFunc(s)
		return nil, err
	}
	family := syscall.AF_UNSPEC
	sotype, err := syscall.GetsockoptInt(s, syscall.SOL_SOCKET, windows.SO_TYPE)
	if err != nil {
		poll.CloseFunc(s)
		return nil, os.NewSyscallError("getsockopt", err)
	}
	lsa, _ := syscall.Getsockname(s)
	rsa, _ := syscall.Getpeername(s)
	switch lsa.(type) {
	case *syscall.SockaddrInet4:
		family = syscall.AF_INET
	case *syscall.SockaddrInet6:
		family = syscall.AF_INET6
	case *syscall.SockaddrUnix:
		poll.CloseFunc(s)
		return nil, syscall.EWINDOWS
	default:
		poll.CloseFunc(s)
		return nil, syscall.EPROTONOSUPPORT
	}
	fd, err := newFD(s, family, sotype, "")
	if err != nil {
		poll.CloseFunc(s)
		return nil, err
	}
	laddr := fd.addrFunc()(lsa)
	raddr := fd.addrFunc()(rsa)
	fd.net = laddr.Network()
	if _, err := fd.pfd.Init("tcp", fd.isFile); err != nil {
		fd.Close()
		return nil, err
	}
	fd.setAddr(laddr, raddr)
	return fd, nil
}

func fileConn(f *os.File) (Conn, error) {
	fd, err := newFileFD(f)
	if err != nil {
		return nil, err
	}
	switch fd.laddr.(type) {
	case *TCPAddr:
		return newTCPConn(fd), nil
	case *UDPAddr:
		return newUDPConn(fd), nil
	case *IPAddr:
		return newIPConn(fd), nil
	case *UnixAddr:
		fd.Close()
		return nil, syscall.EWINDOWS
	}
	fd.Close()
	return nil, syscall.EINVAL
}

func fileListener(f *os.File) (Listener, error) {
	fd, err := newFileFD(f)
	if err != nil {
		return nil, err
	}
	switch fd.laddr.(type) {
	case *TCPAddr:
		return &TCPListener{fd}, nil
	case *UnixAddr:
		fd.Close()
		return nil, syscall.EWINDOWS
	}
	fd.Close()
	return nil, syscall.EINVAL
}

func filePacketConn(f *os.File) (PacketConn, error) {
	fd, err := newFileFD(f)
	if err != nil {
		return nil, err
	}
	switch fd.laddr.(type) {
	case *UDPAddr:
		return newUDPConn(fd), nil
	case *IPAddr:
		return newIPConn(fd), nil
	case *UnixAddr:
		fd.Close()
		return nil, syscall.EWINDOWS
	}
	fd.Close()
	return nil, syscall.EINVAL
}
