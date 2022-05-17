package main

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

func sendCtrlBreak(pid int) {
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
	r, _, e := p.Call(syscall.CTRL_BREAK_EVENT, uintptr(pid))
	if r == 0 {
		fmt.Printf("GenerateConsoleCtrlEvent: %v\n", e)
		return
	}
	fmt.Printf("send ctrl break succ\n")
}

func main() {
	l := len(os.Args)
	if l != 2 {
		fmt.Printf("wrong argument number: %v\n", l)
		return
	}

	pid, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Printf("invalid pid: %v\n", err)
		return
	}
	fmt.Printf("pid: %v\n", pid)

	sendCtrlBreak(1)
}
