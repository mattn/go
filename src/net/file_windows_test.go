// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build windows

package net

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func helperCommandWindows(t *testing.T, s syscall.Handle, args ...string) (*exec.Cmd, *bytes.Buffer) {
	var buf bytes.Buffer
	cs := []string{"-test.run=TestHelperProcessWindows", "--"}
	cs = append(cs, fmt.Sprint(s))
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = &buf
	err := cmd.Start()
	if err != nil {
		t.Fatal(err)
	}

	return cmd, &buf
}

func TestHelperProcessWindows(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	ss, cmd, args := args[0], args[1], args[2:]
	si, _ := strconv.Atoi(ss)
	s := syscall.Handle(si)

	switch cmd {
	case "FileConn":
		c, err := FileConn(os.NewFile(uintptr(s), "sysfile"))
		if err != nil {
			log.Fatal(err)
		}
		var rb [500]byte
		n, err := c.Read(rb[:])
		if err != nil {
			log.Fatal(err)
		}
		n, err = c.Write([]byte(string(rb[:n]) + " world"))
		if err != nil {
			log.Fatal(err)
		}
	case "FileListener":
		l, err := FileListener(os.NewFile(uintptr(s), "sysfile"))
		if err != nil {
			log.Fatal(err)
		}
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		var b [20]byte
		n, err := c.Read(b[:])
		if err != nil {
			log.Fatal(err)
		}
		wb := []byte(string(b[:n]) + " world")
		_, err = c.Write(wb)
		if err != nil {
			log.Fatal(err)
		}
	case "FilePacketConn":
		c, err := FilePacketConn(os.NewFile(uintptr(s), "sysfile"))
		if err != nil {
			log.Fatal(err)
		}

		port, err := strconv.Atoi(args[0])
		if err != nil {
			log.Fatal(err)
		}

		addr := &UDPAddr{IP: IPv4(127, 0, 0, 1), Port: port}
		expect := "hello world"
		_, err = c.WriteTo([]byte(expect), addr)
		if err != nil {
			syscall.Closesocket(s)
			log.Fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n", cmd)
		os.Exit(2)
	}
}

func TestFileConnWindows(t *testing.T) {
	cb := make(chan []byte)

	l, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	host, sport, err := SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(sport)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		client, err := l.Accept()
		if err != nil {
			t.Error(err)
			cb <- []byte("sleepy")
			return
		}
		var b [20]byte
		n, err := client.Read(b[:])
		if err != nil {
			t.Error(err)
			cb <- []byte("hungry")
			return
		}
		cb <- b[:n]
	}()

	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Closesocket(s)

	ip := ParseIP(host).To4()
	sa := &syscall.SockaddrInet4{
		Addr: [4]byte{ip[0], ip[1], ip[2], ip[3]},
		Port: port,
	}

	err = syscall.Connect(s, sa)
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}

	c, err := FileConn(os.NewFile(uintptr(s), "sysfile"))
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	defer c.Close()

	expect := "hello world"
	_, err = c.Write([]byte(expect))
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	got := string(<-cb)
	if got != expect {
		syscall.Closesocket(s)
		t.Fatalf("got %v; want %v", got, expect)
	}
}

func TestFileListenerWindows(t *testing.T) {
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Closesocket(s)

	ip := ParseIP("127.0.0.1").To4()
	sa := &syscall.SockaddrInet4{
		Addr: [4]byte{ip[0], ip[1], ip[2], ip[3]},
		Port: 0,
	}

	err = syscall.Bind(s, sa)
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	err = syscall.Listen(s, syscall.SOMAXCONN)
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}

	var ns syscall.Handle
	p, err := syscall.GetCurrentProcess()
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	err = syscall.DuplicateHandle(p, syscall.Handle(s), p, &ns, 0, false, syscall.DUPLICATE_SAME_ACCESS)
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}

	l, err := FileListener(os.NewFile(uintptr(ns), "sysfile"))
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	defer l.Close()

	expect := "hello world"

	go func() {
		time.Sleep(time.Second)
		c, err := Dial(l.Addr().Network(), l.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		_, err = c.Write([]byte(expect))
		if err != nil {
			t.Error(err)
			return
		}
	}()

	client, err := l.Accept()
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}

	var b [20]byte
	n, err := client.Read(b[:])
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	got := string(b[:n])

	if got != expect {
		syscall.Closesocket(s)
		t.Fatalf("got %v; want %v", got, expect)
	}
}

func TestFilePacketConnWindows(t *testing.T) {
	cb := make(chan []byte)

	l, err := ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Closesocket(s)

	ip := ParseIP("127.0.0.1").To4()
	sa := &syscall.SockaddrInet4{
		Addr: [4]byte{ip[0], ip[1], ip[2], ip[3]},
		Port: 0,
	}

	err = syscall.Bind(s, sa)
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}

	c, err := FilePacketConn(os.NewFile(uintptr(s), "sysfile"))
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	defer c.Close()

	go func() {
		var b [20]byte
		n, _, err := l.ReadFrom(b[:])
		if err != nil {
			t.Error(err)
			cb <- []byte("hungry")
			return
		}
		cb <- b[:n]
	}()

	time.Sleep(time.Second)

	expect := "hello world"
	_, err = c.WriteTo([]byte(expect), l.LocalAddr())
	if err != nil {
		syscall.Closesocket(s)
		t.Fatal(err)
	}
	got := string(<-cb)
	if got != expect {
		syscall.Closesocket(s)
		t.Fatalf("got %v; want %v", got, expect)
	}
}

func TestExternalFileConnWindows(t *testing.T) {
	cb := make(chan []byte)

	l, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		client, err := l.Accept()
		if err != nil {
			t.Error(err)
			cb <- []byte("sleepy")
			return
		}
		_, err = client.Write([]byte("hello"))
		if err != nil {
			t.Error(err)
			cb <- []byte("hungry")
			return
		}

		var b [20]byte
		n, err := client.Read(b[:])
		if err != nil {
			t.Error(err)
			cb <- []byte("angry")
			return
		}
		cb <- b[:n]
	}()

	con, err := Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()

	f, err := con.(*TCPConn).File()
	if err != nil {
		t.Fatal(err)
	}
	s := syscall.Handle(f.Fd())

	p, buf := helperCommandWindows(t, s, "FileConn")
	err = p.Wait()
	if err != nil {
		t.Fatal(err, buf.String())
	}

	expect := "hello world"
	got := string(<-cb)
	if got != expect {
		t.Fatalf("got %v; want %v", got, expect)
	}
}

func TestExternalFileListenerWindows(t *testing.T) {
	l, err := Listen("tcp", "127.0.0.1:8999")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()

	f, err := l.(*TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	s := syscall.Handle(f.Fd())

	p, buf := helperCommandWindows(t, s, "FileListener")

	client, err := Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	client.SetReadDeadline(time.Now().Add(10 * time.Second))

	var rb [20]byte
	n, err := client.Read(rb[:])
	if err != nil {
		t.Fatal(err)
	}

	err = p.Wait()
	if err != nil {
		t.Fatal(err, buf.String())
	}

	expect := "hello world"
	got := string(rb[:n])
	if got != expect {
		t.Fatalf("got %v; want %v", got, expect)
	}
}

func TestExternalFilePacketConnWindows(t *testing.T) {
	cb := make(chan []byte)

	l, err := ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		var b [20]byte
		time.Sleep(time.Second)
		n, _, err := l.ReadFrom(b[:])
		if err != nil {
			t.Error(err)
			cb <- []byte("hungry")
			return
		}
		cb <- b[:n]
	}()

	addr := &UDPAddr{IP: IPv4(127, 0, 0, 1), Port: 0}
	con, err := DialUDP("udp", addr, l.LocalAddr().(*UDPAddr))
	if err != nil {
		t.Fatal(err)
	}

	f, err := con.File()
	if err != nil {
		t.Fatal(err)
	}
	s := syscall.Handle(f.Fd())

	p, buf := helperCommandWindows(t, s, "FilePacketConn", fmt.Sprint(l.LocalAddr().(*UDPAddr).Port))
	err = p.Wait()
	if err != nil {
		t.Fatal(err, buf.String())
	}

	expect := "hello world"
	got := string(<-cb)
	if got != expect {
		t.Fatalf("got %v; want %v", got, expect)
	}
}

func TestTCPConnFileWindows(t *testing.T) {
	cb := make(chan []byte)

	l, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		client, err := l.Accept()
		if err != nil {
			t.Error(err)
			cb <- []byte("sleepy")
			return
		}
		var b [20]byte
		n, err := client.Read(b[:])
		if err != nil {
			t.Error(err)
			cb <- []byte("hungry")
			return
		}
		cb <- b[:n]
	}()

	c, err := Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	f, err := c.(*TCPConn).File()
	if err != nil {
		t.Fatal(err)
	}

	expect := "hello world"
	_, err = f.WriteString(expect)
	if err != nil {
		t.Fatal(err)
	}

	got := string(<-cb)
	if got != expect {
		t.Fatalf("got %v; want %v", got, expect)
	}
}
