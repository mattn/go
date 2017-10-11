// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build windows

package plugin

//import "C"

import (
	"debug/pe"
	"errors"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

func open(name string) (*Plugin, error) {
	path, err := filepath.EvalSymlinks(name)
	if err != nil {
		return nil, errors.New("plugin.Open(" + name + "): EvalSymlinks failed")
	}

	pluginsMu.Lock()
	if p := plugins[path]; p != nil {
		pluginsMu.Unlock()
		<-p.loaded
		return p, nil
	}

	f, err := pe.Open(path)
	if err != nil {
		pluginsMu.Unlock()
		return nil, errors.New("plugin.Open: " + err.Error())
	}
	names, err := f.ImportedSymbols()
	if err != nil {
		pluginsMu.Unlock()
		return nil, errors.New("plugin.Open: " + err.Error())
	}
	f.Close()

	h, err := syscall.LoadLibrary(path)
	if err != nil {
		pluginsMu.Unlock()
		return nil, errors.New("plugin.Open: " + err.Error())
	}
	if len(name) > 4 && name[len(name)-4:] == ".dll" {
		name = name[:len(name)-4]
	}
	if plugins == nil {
		plugins = make(map[string]*Plugin)
	}
	/*
		pluginpath, syms, errstr := lastmoduleinit()
		if errstr != "" {
			plugins[path] = &Plugin{
				pluginpath: path,
				err:        errstr,
			}
			pluginsMu.Unlock()
			return nil, errors.New(`plugin.Open("` + name + `"): ` + errstr)
		}
	*/
	// This function can be called from the init function of a plugin.
	// Drop a placeholder in the map so subsequent opens can wait on it.
	p := &Plugin{
		pluginpath: path,
		loaded:     make(chan struct{}),
		syms:       make(map[string]interface{}),
	}
	plugins[path] = p
	pluginsMu.Unlock()

	initStr := name + ".init"
	initFuncPC, _ := syscall.GetProcAddress(h, initStr)
	if initFuncPC != 0 {
		initFunc := *(*func())(unsafe.Pointer(&initFuncPC))
		initFunc()
	}

	// Fill out the value of each plugin symbol.
	for _, symName := range names {
		println(symName)
		isFunc := symName[0] == '.'
		if isFunc {
			delete(p.syms, symName)
			symName = symName[1:]
		}

		symNameStr := name + "." + symName
		symPC, _ := syscall.GetProcAddress(h, symNameStr)
		if err != nil {
			return nil, errors.New("plugin.Open: could not find symbol " + symName + ": " + err.Error())
		}
		valp := (*[2]unsafe.Pointer)(unsafe.Pointer(&symPC))
		if isFunc {
			(*valp)[1] = unsafe.Pointer(&symPC)
		} else {
			(*valp)[1] = unsafe.Pointer(&p)
		}
		p.syms[symName] = symPC
	}
	close(p.loaded)
	return p, nil
}

func lookup(p *Plugin, symName string) (Symbol, error) {
	if s := p.syms[symName]; s != nil {
		return s, nil
	}
	return nil, errors.New("plugin: symbol " + symName + " not found in plugin " + p.pluginpath)
}

var (
	pluginsMu sync.Mutex
	plugins   map[string]*Plugin
)
