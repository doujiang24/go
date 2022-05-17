package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func sendCtrlBreak2(pid int) {
	d, e := syscall.LoadDLL("kernel32.dll")
	if e != nil {
		fmt.Printf("LoadDLL: %v\n", e)
		return
	}
	p, e := d.FindProc("GenerateConsoleCtrlEvent")
	if e != nil {
		fmt.Printf("FindProc: %v\n", e)
		return
	}
	r, _, e := p.Call(syscall.CTRL_C_EVENT, uintptr(pid))
	if r == 0 {
		fmt.Printf("GenerateConsoleCtrlEvent: %v\n", e)
		return
	}
	fmt.Printf("send ctrl break succ\n")
}

func main() {
	// create source file
	const source = `
package main

import (
	"log"
	"os"
	"os/signal"
	"time"
)


func main() {
	c := make(chan os.Signal, 10)
	signal.Notify(c)
	select {
	case s := <-c:
		if s != os.Interrupt {
			log.Fatalf("Wrong signal received: got %q, want %q\n", s, os.Interrupt)
		}
	case <-time.After(3 * time.Second):
		log.Fatalf("Timeout waiting for Ctrl+Break\n")
	}
}
`
	// write ctrlbreak.go
	name := "ctlbreak"
	src := name + ".go"
	f, err := os.Create(src)
	if err != nil {
		fmt.Printf("Failed to create %v: %v", src, err)
		return
	}
	defer f.Close()
	f.Write([]byte(source))

	// compile it
	exe := name + ".exe"
	defer os.Remove(exe)
	o, err := exec.Command("go", "build", "-o", exe, src).CombinedOutput()
	if err != nil {
		fmt.Printf("Failed to compile: %v\n%v", err, string(o))
		return
	}

	// run it
	cmd := exec.Command(exe)
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	err = cmd.Start()
	if err != nil {
		fmt.Printf("Start failed: %v", err)
		return
	}
	go func() {
		time.Sleep(1 * time.Second)
		sendCtrlBreak2(cmd.Process.Pid)
	}()
	err = cmd.Wait()
	if err != nil {
		fmt.Printf("Program exited with error: %v\n%v", err, string(b.Bytes()))
		return
	}
	fmt.Printf("ok\n")
}
