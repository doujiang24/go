// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package carchive_test

import (
	"bytes"
	"debug/elf"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unicode"
)

// Program to run.
var bin []string

// C compiler with args (from $(go env CC) $(go env GOGCCFLAGS)).
var cc []string

// ".exe" on Windows.
var exeSuffix string

var GOOS, GOARCH, GOPATH string
var libgodir string

var testWork bool // If true, preserve temporary directories.

func TestMain(m *testing.M) {
	flag.BoolVar(&testWork, "testwork", false, "if true, log and preserve the test's temporary working directory")
	flag.Parse()
	if testing.Short() && os.Getenv("GO_BUILDER_NAME") == "" {
		fmt.Printf("SKIP - short mode and $GO_BUILDER_NAME not set\n")
		os.Exit(0)
	}
	log.SetFlags(log.Lshortfile)
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	// We need a writable GOPATH in which to run the tests.
	// Construct one in a temporary directory.
	var err error
	GOPATH, err = os.MkdirTemp("", "carchive_test")
	if err != nil {
		log.Panic(err)
	}
	if testWork {
		log.Println(GOPATH)
	} else {
		defer os.RemoveAll(GOPATH)
	}
	os.Setenv("GOPATH", GOPATH)

	// Copy testdata into GOPATH/src/testarchive, along with a go.mod file
	// declaring the same path.
	modRoot := filepath.Join(GOPATH, "src", "testcarchive")
	if err := overlayDir(modRoot, "testdata"); err != nil {
		log.Panic(err)
	}
	if err := os.Chdir(modRoot); err != nil {
		log.Panic(err)
	}
	os.Setenv("PWD", modRoot)
	if err := os.WriteFile("go.mod", []byte("module testcarchive\n"), 0666); err != nil {
		log.Panic(err)
	}

	GOOS = goEnv("GOOS")
	GOARCH = goEnv("GOARCH")
	bin = cmdToRun("./testp")

	ccOut := goEnv("CC")
	cc = []string{string(ccOut)}

	out := goEnv("GOGCCFLAGS")
	quote := '\000'
	start := 0
	lastSpace := true
	backslash := false
	s := string(out)
	for i, c := range s {
		if quote == '\000' && unicode.IsSpace(c) {
			if !lastSpace {
				cc = append(cc, s[start:i])
				lastSpace = true
			}
		} else {
			if lastSpace {
				start = i
				lastSpace = false
			}
			if quote == '\000' && !backslash && (c == '"' || c == '\'') {
				quote = c
				backslash = false
			} else if !backslash && quote == c {
				quote = '\000'
			} else if (quote == '\000' || quote == '"') && !backslash && c == '\\' {
				backslash = true
			} else {
				backslash = false
			}
		}
	}
	if !lastSpace {
		cc = append(cc, s[start:])
	}

	if GOOS == "aix" {
		// -Wl,-bnoobjreorder is mandatory to keep the same layout
		// in .text section.
		cc = append(cc, "-Wl,-bnoobjreorder")
	}
	libbase := GOOS + "_" + GOARCH
	if runtime.Compiler == "gccgo" {
		libbase = "gccgo_" + libgodir + "_fPIC"
	} else {
		switch GOOS {
		case "darwin", "ios":
			if GOARCH == "arm64" {
				libbase += "_shared"
			}
		case "dragonfly", "freebsd", "linux", "netbsd", "openbsd", "solaris", "illumos":
			libbase += "_shared"
		}
	}
	libgodir = filepath.Join(GOPATH, "pkg", libbase, "testcarchive")
	cc = append(cc, "-I", libgodir)

	// Force reallocation (and avoid aliasing bugs) for parallel tests that append to cc.
	cc = cc[:len(cc):len(cc)]

	if GOOS == "windows" {
		exeSuffix = ".exe"
	}

	return m.Run()
}

func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "%s", ee.Stderr)
		}
		log.Panicf("go env %s failed:\n%s\n", key, err)
	}
	return strings.TrimSpace(string(out))
}

func cmdToRun(name string) []string {
	execScript := "go_" + goEnv("GOOS") + "_" + goEnv("GOARCH") + "_exec"
	executor, err := exec.LookPath(execScript)
	if err != nil {
		return []string{name}
	}
	return []string{executor, name}
}

// genHeader writes a C header file for the C-exported declarations found in .go
// source files in dir.
//
// TODO(golang.org/issue/35715): This should be simpler.
func genHeader(t *testing.T, header, dir string) {
	t.Helper()

	// The 'cgo' command generates a number of additional artifacts,
	// but we're only interested in the header.
	// Shunt the rest of the outputs to a temporary directory.
	objDir, err := os.MkdirTemp(GOPATH, "_obj")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(objDir)

	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "tool", "cgo",
		"-objdir", objDir,
		"-exportheader", header)
	cmd.Args = append(cmd.Args, files...)
	t.Log(cmd.Args)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}
}

func testInstall(t *testing.T, exe, libgoa, libgoh string, buildcmd ...string) {
	t.Helper()
	cmd := exec.Command(buildcmd[0], buildcmd[1:]...)
	t.Log(buildcmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}
	if !testWork {
		defer func() {
			os.Remove(libgoa)
			os.Remove(libgoh)
		}()
	}

	ccArgs := append(cc, "-o", exe, "main.c")
	if GOOS == "windows" {
		ccArgs = append(ccArgs, "main_windows.c", libgoa, "-lntdll", "-lws2_32", "-lwinmm")
	} else {
		ccArgs = append(ccArgs, "main_unix.c", libgoa)
	}
	if runtime.Compiler == "gccgo" {
		ccArgs = append(ccArgs, "-lgo")
	}
	t.Log(ccArgs)
	if out, err := exec.Command(ccArgs[0], ccArgs[1:]...).CombinedOutput(); err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}
	if !testWork {
		defer os.Remove(exe)
	}

	binArgs := append(cmdToRun(exe), "arg1", "arg2")
	cmd = exec.Command(binArgs[0], binArgs[1:]...)
	if runtime.Compiler == "gccgo" {
		cmd.Env = append(os.Environ(), "GCCGO=1")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}

	checkLineComments(t, libgoh)
}

var badLineRegexp = regexp.MustCompile(`(?m)^#line [0-9]+ "/.*$`)

// checkLineComments checks that the export header generated by
// -buildmode=c-archive doesn't have any absolute paths in the #line
// comments. We don't want those paths because they are unhelpful for
// the user and make the files change based on details of the location
// of GOPATH.
func checkLineComments(t *testing.T, hdrname string) {
	hdr, err := os.ReadFile(hdrname)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Error(err)
		}
		return
	}
	if line := badLineRegexp.Find(hdr); line != nil {
		t.Errorf("bad #line directive with absolute path in %s: %q", hdrname, line)
	}
}

// checkArchive verifies that the created library looks OK.
// We just check a couple of things now, we can add more checks as needed.
func checkArchive(t *testing.T, arname string) {
	t.Helper()

	switch GOOS {
	case "aix", "darwin", "ios", "windows":
		// We don't have any checks for non-ELF libraries yet.
		if _, err := os.Stat(arname); err != nil {
			t.Errorf("archive %s does not exist: %v", arname, err)
		}
	default:
		checkELFArchive(t, arname)
	}
}

// checkELFArchive checks an ELF archive.
func checkELFArchive(t *testing.T, arname string) {
	t.Helper()

	f, err := os.Open(arname)
	if err != nil {
		t.Errorf("archive %s does not exist: %v", arname, err)
		return
	}
	defer f.Close()

	// TODO(iant): put these in a shared package?  But where?
	const (
		magic = "!<arch>\n"
		fmag  = "`\n"

		namelen = 16
		datelen = 12
		uidlen  = 6
		gidlen  = 6
		modelen = 8
		sizelen = 10
		fmaglen = 2
		hdrlen  = namelen + datelen + uidlen + gidlen + modelen + sizelen + fmaglen
	)

	type arhdr struct {
		name string
		date string
		uid  string
		gid  string
		mode string
		size string
		fmag string
	}

	var magbuf [len(magic)]byte
	if _, err := io.ReadFull(f, magbuf[:]); err != nil {
		t.Errorf("%s: archive too short", arname)
		return
	}
	if string(magbuf[:]) != magic {
		t.Errorf("%s: incorrect archive magic string %q", arname, magbuf)
	}

	off := int64(len(magic))
	for {
		if off&1 != 0 {
			var b [1]byte
			if _, err := f.Read(b[:]); err != nil {
				if err == io.EOF {
					break
				}
				t.Errorf("%s: error skipping alignment byte at %d: %v", arname, off, err)
			}
			off++
		}

		var hdrbuf [hdrlen]byte
		if _, err := io.ReadFull(f, hdrbuf[:]); err != nil {
			if err == io.EOF {
				break
			}
			t.Errorf("%s: error reading archive header at %d: %v", arname, off, err)
			return
		}

		var hdr arhdr
		hdrslice := hdrbuf[:]
		set := func(len int, ps *string) {
			*ps = string(bytes.TrimSpace(hdrslice[:len]))
			hdrslice = hdrslice[len:]
		}
		set(namelen, &hdr.name)
		set(datelen, &hdr.date)
		set(uidlen, &hdr.uid)
		set(gidlen, &hdr.gid)
		set(modelen, &hdr.mode)
		set(sizelen, &hdr.size)
		hdr.fmag = string(hdrslice[:fmaglen])
		hdrslice = hdrslice[fmaglen:]
		if len(hdrslice) != 0 {
			t.Fatalf("internal error: len(hdrslice) == %d", len(hdrslice))
		}

		if hdr.fmag != fmag {
			t.Errorf("%s: invalid fmagic value %q at %d", arname, hdr.fmag, off)
			return
		}

		size, err := strconv.ParseInt(hdr.size, 10, 64)
		if err != nil {
			t.Errorf("%s: error parsing size %q at %d: %v", arname, hdr.size, off, err)
			return
		}

		off += hdrlen

		switch hdr.name {
		case "__.SYMDEF", "/", "/SYM64/":
			// The archive symbol map.
		case "//", "ARFILENAMES/":
			// The extended name table.
		default:
			// This should be an ELF object.
			checkELFArchiveObject(t, arname, off, io.NewSectionReader(f, off, size))
		}

		off += size
		if _, err := f.Seek(off, os.SEEK_SET); err != nil {
			t.Errorf("%s: failed to seek to %d: %v", arname, off, err)
		}
	}
}

// checkELFArchiveObject checks an object in an ELF archive.
func checkELFArchiveObject(t *testing.T, arname string, off int64, obj io.ReaderAt) {
	t.Helper()

	ef, err := elf.NewFile(obj)
	if err != nil {
		t.Errorf("%s: failed to open ELF file at %d: %v", arname, off, err)
		return
	}
	defer ef.Close()

	// Verify section types.
	for _, sec := range ef.Sections {
		want := elf.SHT_NULL
		switch sec.Name {
		case ".text", ".data":
			want = elf.SHT_PROGBITS
		case ".bss":
			want = elf.SHT_NOBITS
		case ".symtab":
			want = elf.SHT_SYMTAB
		case ".strtab":
			want = elf.SHT_STRTAB
		case ".init_array":
			want = elf.SHT_INIT_ARRAY
		case ".fini_array":
			want = elf.SHT_FINI_ARRAY
		case ".preinit_array":
			want = elf.SHT_PREINIT_ARRAY
		}
		if want != elf.SHT_NULL && sec.Type != want {
			t.Errorf("%s: incorrect section type in elf file at %d for section %q: got %v want %v", arname, off, sec.Name, sec.Type, want)
		}
	}
}

func TestSignalForwarding(t *testing.T) {
	checkSignalForwardingTest(t)

	/*
		if !testWork {
			defer func() {
				os.Remove("libgo2.a")
				os.Remove("libgo2.h")
				os.Remove("testp" + exeSuffix)
				os.RemoveAll(filepath.Join(GOPATH, "pkg"))
			}()
		}
	*/

	dir, _ := os.Getwd()
	t.Logf("current dir: %v", dir)

	cmd := exec.Command("go", "build", "-buildmode=c-archive", "-o", "libgo2.a", "./libgo2")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}
	checkLineComments(t, "libgo2.h")
	checkArchive(t, "libgo2.a")

	ccArgs := append(cc, "-o", "testp"+exeSuffix, "main5.c", "libgo2.a")
	if runtime.Compiler == "gccgo" {
		ccArgs = append(ccArgs, "-lgo")
	}
	if out, err := exec.Command(ccArgs[0], ccArgs[1:]...).CombinedOutput(); err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}

	t.Logf("compile cmd: %v", strings.Join(ccArgs, " "))

	cmd = exec.Command(bin[0], append(bin[1:], "1", "-v")...)

	t.Logf("test cmd: %v", strings.Join(bin, " "))

	out, err := cmd.CombinedOutput()
	t.Logf("%v\n%s", cmd.Args, out)
	expectSignal(t, err, syscall.SIGSEGV)
}

// checkSignalForwardingTest calls t.Skip if the SignalForwarding test
// doesn't work on this platform.
func checkSignalForwardingTest(t *testing.T) {
	switch GOOS {
	case "darwin", "ios":
		switch GOARCH {
		case "arm64":
			t.Skipf("skipping on %s/%s; see https://golang.org/issue/13701", GOOS, GOARCH)
		}
	case "windows":
		t.Skip("skipping signal test on Windows")
	}
}

// expectSignal checks that err, the exit status of a test program,
// shows a failure due to a specific signal. Returns whether we found
// the expected signal.
func expectSignal(t *testing.T, err error, sig syscall.Signal) bool {
	if err == nil {
		t.Error("test program succeeded unexpectedly")
	} else if ee, ok := err.(*exec.ExitError); !ok {
		t.Errorf("error (%v) has type %T; expected exec.ExitError", err, err)
	} else if ws, ok := ee.Sys().(syscall.WaitStatus); !ok {
		t.Errorf("error.Sys (%v) has type %T; expected syscall.WaitStatus", ee.Sys(), ee.Sys())
	} else if !ws.Signaled() || ws.Signal() != sig {
		t.Errorf("got %v; expected signal %v", ee, sig)
	} else {
		return true
	}
	return false
}

const testar = `#!/usr/bin/env bash
while [[ $1 == -* ]] >/dev/null; do
  shift
done
echo "testar" > $1
echo "testar" > PWD/testar.ran
`

func hasDynTag(t *testing.T, f *elf.File, tag elf.DynTag) bool {
	ds := f.SectionByType(elf.SHT_DYNAMIC)
	if ds == nil {
		t.Error("no SHT_DYNAMIC section")
		return false
	}
	d, err := ds.Data()
	if err != nil {
		t.Errorf("can't read SHT_DYNAMIC contents: %v", err)
		return false
	}
	for len(d) > 0 {
		var t elf.DynTag
		switch f.Class {
		case elf.ELFCLASS32:
			t = elf.DynTag(f.ByteOrder.Uint32(d[:4]))
			d = d[8:]
		case elf.ELFCLASS64:
			t = elf.DynTag(f.ByteOrder.Uint64(d[:8]))
			d = d[16:]
		}
		if t == tag {
			return true
		}
	}
	return false
}
