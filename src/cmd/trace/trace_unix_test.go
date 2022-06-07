// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"bytes"
	"cmd/internal/traceviewer"
	"fmt"
	traceparser "internal/trace"
	"io"
	"runtime"
	"runtime/trace"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestGoroutineInSyscall tests threads for timer goroutines
// that preexisted when the tracing started were not counted
// as threads in syscall. See golang.org/issues/22574.
func TestGoroutineInSyscall(t *testing.T) {
	// Start one goroutine blocked in syscall.
	//
	// TODO: syscall.Pipe used to cause the goroutine to
	// remain blocked in syscall is not portable. Replace
	// it with a more portable way so this test can run
	// on non-unix architecture e.g. Windows.
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	var wg sync.WaitGroup
	defer func() {
		syscall.Write(p[1], []byte("a"))
		wg.Wait()

		syscall.Close(p[0])
		syscall.Close(p[1])
	}()
	wg.Add(1)
	go func() {
		var tmp [1]byte
		syscall.Read(p[0], tmp[:])
		wg.Done()
	}()

	// Start multiple timer goroutines.
	allTimers := make([]*time.Timer, 2*runtime.GOMAXPROCS(0))
	defer func() {
		for _, timer := range allTimers {
			timer.Stop()
		}
	}()

	var timerSetup sync.WaitGroup
	for i := range allTimers {
		timerSetup.Add(1)
		go func(i int) {
			defer timerSetup.Done()
			allTimers[i] = time.AfterFunc(time.Hour, nil)
		}(i)
	}
	timerSetup.Wait()

	// Collect and parse trace.
	buf := new(bytes.Buffer)
	if err := trace.Start(buf); err != nil {
		t.Fatalf("failed to start tracing: %v", err)
	}
	trace.Stop()

	res, err := traceparser.Parse(buf, "")
	if err == traceparser.ErrTimeOrder {
		t.Skipf("skipping due to golang.org/issue/16755 (timestamps are unreliable): %v", err)
	} else if err != nil {
		t.Fatalf("failed to parse trace: %v", err)
	}

	// Check only one thread for the pipe read goroutine is
	// considered in-syscall.
	c := viewerDataTraceConsumer(io.Discard, 0, 1<<63-1)
	c.consumeViewerEvent = func(ev *traceviewer.Event, _ bool) {
		t.Logf("ev.Name: %v", ev.Name)
		if ev.Name == "Goroutines" {
			arg := ev.Arg.(*goroutineCountersArg)
			t.Logf("at time %v, running: %d, runable: %d, gcwaiting: %d", ev.Time, arg.Running, arg.Runnable, arg.GCWaiting)
		}
		if ev.Name == "Threads" {
			arg := ev.Arg.(*threadCountersArg)
			t.Logf("%d threads in syscall at time %v", arg.InSyscall, ev.Time)
			if arg.InSyscall > 1 {
				t.Errorf("%d threads in syscall at time %v; want less than 1 thread in syscall", arg.InSyscall, ev.Time)
			}
		}
	}

	param := &traceParams{
		parsed:  res,
		endTime: int64(1<<63 - 1),
	}

	if err := generateTrace2(param, c, t); err != nil {
		t.Fatalf("failed to generate ViewerData: %v", err)
	}
}
func generateTrace2(params *traceParams, consumer traceConsumer, t *testing.T) error {
	defer consumer.flush()

	ctx := &traceContext{traceParams: params}
	ctx.frameTree.children = make(map[uint64]frameNode)
	ctx.consumer = consumer

	ctx.consumer.consumeTimeUnit("ns")
	maxProc := 0
	ginfos := make(map[uint64]*gInfo)
	stacks := params.parsed.Stacks

	getGInfo := func(g uint64) *gInfo {
		info, ok := ginfos[g]
		if !ok {
			info = &gInfo{}
			ginfos[g] = info
		}
		return info
	}

	// Since we make many calls to setGState, we record a sticky
	// error in setGStateErr and check it after every event.
	var setGStateErr error
	setGState := func(ev *traceparser.Event, g uint64, oldState, newState gState) {
		info := getGInfo(g)
		if oldState == gWaiting && info.state == gWaitingGC {
			// For checking, gWaiting counts as any gWaiting*.
			oldState = info.state
		}
		if info.state != oldState && setGStateErr == nil {
			setGStateErr = fmt.Errorf("expected G %d to be in state %d, but got state %d", g, oldState, newState)
		}
		ctx.gstates[info.state]--
		ctx.gstates[newState]++
		info.state = newState
	}

	for _, ev := range ctx.parsed.Events {
		// Handle state transitions before we filter out events.
		t.Logf("ev.Type: %v", ev.Type)
		switch ev.Type {
		case traceparser.EvGoStart, traceparser.EvGoStartLabel:
			setGState(ev, ev.G, gRunnable, gRunning)
			info := getGInfo(ev.G)
			info.start = ev
		case traceparser.EvProcStart:
			ctx.threadStats.prunning++
		case traceparser.EvProcStop:
			ctx.threadStats.prunning--
		case traceparser.EvGoCreate:
			newG := ev.Args[0]
			info := getGInfo(newG)
			if info.name != "" {
				return fmt.Errorf("duplicate go create event for go id=%d detected at offset %d", newG, ev.Off)
			}

			stk, ok := stacks[ev.Args[1]]
			if !ok || len(stk) == 0 {
				return fmt.Errorf("invalid go create event: missing stack information for go id=%d at offset %d", newG, ev.Off)
			}

			fname := stk[0].Fn
			info.name = fmt.Sprintf("G%v %s", newG, fname)
			info.isSystemG = isSystemGoroutine(fname)

			ctx.gcount++

			t.Logf("fname: %v, info.name: %v, info.isSystemG: %v, ctx.gcount: %v", fname, info.name, info.isSystemG, ctx.gcount)
			setGState(ev, newG, gDead, gRunnable)
		case traceparser.EvGoEnd:
			ctx.gcount--
			setGState(ev, ev.G, gRunning, gDead)
		case traceparser.EvGoUnblock:
			setGState(ev, ev.Args[0], gWaiting, gRunnable)
		case traceparser.EvGoSysExit:
			setGState(ev, ev.G, gWaiting, gRunnable)
			if getGInfo(ev.G).isSystemG {
				ctx.threadStats.insyscallRuntime--
			} else {
				ctx.threadStats.insyscall--
			}
		case traceparser.EvGoSysBlock:
			setGState(ev, ev.G, gRunning, gWaiting)
			if getGInfo(ev.G).isSystemG {
				ctx.threadStats.insyscallRuntime++
			} else {
				ctx.threadStats.insyscall++
			}
		case traceparser.EvGoSched, traceparser.EvGoPreempt:
			setGState(ev, ev.G, gRunning, gRunnable)
		case traceparser.EvGoStop,
			traceparser.EvGoSleep, traceparser.EvGoBlock, traceparser.EvGoBlockSend, traceparser.EvGoBlockRecv,
			traceparser.EvGoBlockSelect, traceparser.EvGoBlockSync, traceparser.EvGoBlockCond, traceparser.EvGoBlockNet:
			setGState(ev, ev.G, gRunning, gWaiting)
		case traceparser.EvGoBlockGC:
			setGState(ev, ev.G, gRunning, gWaitingGC)
		case traceparser.EvGCMarkAssistStart:
			getGInfo(ev.G).markAssist = ev
		case traceparser.EvGCMarkAssistDone:
			getGInfo(ev.G).markAssist = nil
		case traceparser.EvGoWaiting:
			setGState(ev, ev.G, gRunnable, gWaiting)
		case traceparser.EvGoInSyscall:
			// Cancel out the effect of EvGoCreate at the beginning.
			setGState(ev, ev.G, gRunnable, gWaiting)
			if getGInfo(ev.G).isSystemG {
				ctx.threadStats.insyscallRuntime++
			} else {
				ctx.threadStats.insyscall++
			}
		case traceparser.EvHeapAlloc:
			ctx.heapStats.heapAlloc = ev.Args[0]
		case traceparser.EvHeapGoal:
			ctx.heapStats.nextGC = ev.Args[0]
		}
		if setGStateErr != nil {
			return setGStateErr
		}
		if ctx.gstates[gRunnable] < 0 || ctx.gstates[gRunning] < 0 || ctx.threadStats.insyscall < 0 || ctx.threadStats.insyscallRuntime < 0 {
			return fmt.Errorf("invalid state after processing %v: runnable=%d running=%d insyscall=%d insyscallRuntime=%d", ev, ctx.gstates[gRunnable], ctx.gstates[gRunning], ctx.threadStats.insyscall, ctx.threadStats.insyscallRuntime)
		}

		// Ignore events that are from uninteresting goroutines
		// or outside of the interesting timeframe.
		if ctx.gs != nil && ev.P < traceparser.FakeP && !ctx.gs[ev.G] {
			continue
		}
		if !withinTimeRange(ev, ctx.startTime, ctx.endTime) {
			continue
		}

		if ev.P < traceparser.FakeP && ev.P > maxProc {
			maxProc = ev.P
		}

		// Emit trace objects.
		switch ev.Type {
		case traceparser.EvProcStart:
			if ctx.mode&modeGoroutineOriented != 0 {
				continue
			}
			ctx.emitInstant(ev, "proc start", "")
		case traceparser.EvProcStop:
			if ctx.mode&modeGoroutineOriented != 0 {
				continue
			}
			ctx.emitInstant(ev, "proc stop", "")
		case traceparser.EvGCStart:
			ctx.emitSlice(ev, "GC")
		case traceparser.EvGCDone:
		case traceparser.EvGCSTWStart:
			if ctx.mode&modeGoroutineOriented != 0 {
				continue
			}
			ctx.emitSlice(ev, fmt.Sprintf("STW (%s)", ev.SArgs[0]))
		case traceparser.EvGCSTWDone:
		case traceparser.EvGCMarkAssistStart:
			// Mark assists can continue past preemptions, so truncate to the
			// whichever comes first. We'll synthesize another slice if
			// necessary in EvGoStart.
			markFinish := ev.Link
			goFinish := getGInfo(ev.G).start.Link
			fakeMarkStart := *ev
			text := "MARK ASSIST"
			if markFinish == nil || markFinish.Ts > goFinish.Ts {
				fakeMarkStart.Link = goFinish
				text = "MARK ASSIST (unfinished)"
			}
			ctx.emitSlice(&fakeMarkStart, text)
		case traceparser.EvGCSweepStart:
			slice := ctx.makeSlice(ev, "SWEEP")
			if done := ev.Link; done != nil && done.Args[0] != 0 {
				slice.Arg = struct {
					Swept     uint64 `json:"Swept bytes"`
					Reclaimed uint64 `json:"Reclaimed bytes"`
				}{done.Args[0], done.Args[1]}
			}
			ctx.emit(slice)
		case traceparser.EvGoStart, traceparser.EvGoStartLabel:
			info := getGInfo(ev.G)
			if ev.Type == traceparser.EvGoStartLabel {
				ctx.emitSlice(ev, ev.SArgs[0])
			} else {
				ctx.emitSlice(ev, info.name)
			}
			if info.markAssist != nil {
				// If we're in a mark assist, synthesize a new slice, ending
				// either when the mark assist ends or when we're descheduled.
				markFinish := info.markAssist.Link
				goFinish := ev.Link
				fakeMarkStart := *ev
				text := "MARK ASSIST (resumed, unfinished)"
				if markFinish != nil && markFinish.Ts < goFinish.Ts {
					fakeMarkStart.Link = markFinish
					text = "MARK ASSIST (resumed)"
				}
				ctx.emitSlice(&fakeMarkStart, text)
			}
		case traceparser.EvGoCreate:
			ctx.emitArrow(ev, "go")
		case traceparser.EvGoUnblock:
			ctx.emitArrow(ev, "unblock")
		case traceparser.EvGoSysCall:
			ctx.emitInstant(ev, "syscall", "")
		case traceparser.EvGoSysExit:
			ctx.emitArrow(ev, "sysexit")
		case traceparser.EvUserLog:
			ctx.emitInstant(ev, formatUserLog(ev), "user event")
		case traceparser.EvUserTaskCreate:
			ctx.emitInstant(ev, "task start", "user event")
		case traceparser.EvUserTaskEnd:
			ctx.emitInstant(ev, "task end", "user event")
		}
		// Emit any counter updates.
		ctx.emitThreadCounters(ev)
		ctx.emitHeapCounters(ev)
		ctx.emitGoroutineCounters(ev)
	}

	ctx.emitSectionFooter(statsSection, "STATS", 0)

	if ctx.mode&modeTaskOriented != 0 {
		ctx.emitSectionFooter(tasksSection, "TASKS", 1)
	}

	if ctx.mode&modeGoroutineOriented != 0 {
		ctx.emitSectionFooter(procsSection, "G", 2)
	} else {
		ctx.emitSectionFooter(procsSection, "PROCS", 2)
	}

	ctx.emitFooter(&traceviewer.Event{Name: "thread_name", Phase: "M", PID: procsSection, TID: traceparser.GCP, Arg: &NameArg{"GC"}})
	ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: traceparser.GCP, Arg: &SortIndexArg{-6}})

	ctx.emitFooter(&traceviewer.Event{Name: "thread_name", Phase: "M", PID: procsSection, TID: traceparser.NetpollP, Arg: &NameArg{"Network"}})
	ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: traceparser.NetpollP, Arg: &SortIndexArg{-5}})

	ctx.emitFooter(&traceviewer.Event{Name: "thread_name", Phase: "M", PID: procsSection, TID: traceparser.TimerP, Arg: &NameArg{"Timers"}})
	ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: traceparser.TimerP, Arg: &SortIndexArg{-4}})

	ctx.emitFooter(&traceviewer.Event{Name: "thread_name", Phase: "M", PID: procsSection, TID: traceparser.SyscallP, Arg: &NameArg{"Syscalls"}})
	ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: traceparser.SyscallP, Arg: &SortIndexArg{-3}})

	// Display rows for Ps if we are in the default trace view mode (not goroutine-oriented presentation)
	if ctx.mode&modeGoroutineOriented == 0 {
		for i := 0; i <= maxProc; i++ {
			ctx.emitFooter(&traceviewer.Event{Name: "thread_name", Phase: "M", PID: procsSection, TID: uint64(i), Arg: &NameArg{fmt.Sprintf("Proc %v", i)}})
			ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: uint64(i), Arg: &SortIndexArg{i}})
		}
	}

	// Display task and its regions if we are in task-oriented presentation mode.
	if ctx.mode&modeTaskOriented != 0 {
		// sort tasks based on the task start time.
		sortedTask := make([]*taskDesc, 0, len(ctx.tasks))
		for _, task := range ctx.tasks {
			sortedTask = append(sortedTask, task)
		}
		sort.SliceStable(sortedTask, func(i, j int) bool {
			ti, tj := sortedTask[i], sortedTask[j]
			if ti.firstTimestamp() == tj.firstTimestamp() {
				return ti.lastTimestamp() < tj.lastTimestamp()
			}
			return ti.firstTimestamp() < tj.firstTimestamp()
		})

		for i, task := range sortedTask {
			ctx.emitTask(task, i)

			// If we are in goroutine-oriented mode, we draw regions.
			// TODO(hyangah): add this for task/P-oriented mode (i.e., focustask view) too.
			if ctx.mode&modeGoroutineOriented != 0 {
				for _, s := range task.regions {
					ctx.emitRegion(s)
				}
			}
		}
	}

	// Display goroutine rows if we are either in goroutine-oriented mode.
	if ctx.mode&modeGoroutineOriented != 0 {
		for k, v := range ginfos {
			if !ctx.gs[k] {
				continue
			}
			ctx.emitFooter(&traceviewer.Event{Name: "thread_name", Phase: "M", PID: procsSection, TID: k, Arg: &NameArg{v.name}})
		}
		// Row for the main goroutine (maing)
		ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: ctx.maing, Arg: &SortIndexArg{-2}})
		// Row for GC or global state (specified with G=0)
		ctx.emitFooter(&traceviewer.Event{Name: "thread_sort_index", Phase: "M", PID: procsSection, TID: 0, Arg: &SortIndexArg{-1}})
	}

	return nil
}
