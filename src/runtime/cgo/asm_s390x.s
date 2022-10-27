// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"

TEXT cgo_crosscall(SB),NOSPLIT,$0-0
	JMP	crosscall2(SB)

// Called by C code generated by cmd/cgo.
// func crosscall2(fn, a unsafe.Pointer, n int32, ctxt uintptr)
// Saves C callee-saved registers and calls cgocallback with three arguments.
// fn is the PC of a func(a unsafe.Pointer) function.
TEXT crosscall2(SB),NOSPLIT|NOFRAME,$0
	// Start with standard C stack frame layout and linkage.

	// Save R6-R15 in the register save area of the calling function.
	STMG	R6, R15, 48(R15)

	// Allocate 96 bytes on the stack.
	MOVD	$-96(R15), R15

	// Save F8-F15 in our stack frame.
	FMOVD	F8, 32(R15)
	FMOVD	F9, 40(R15)
	FMOVD	F10, 48(R15)
	FMOVD	F11, 56(R15)
	FMOVD	F12, 64(R15)
	FMOVD	F13, 72(R15)
	FMOVD	F14, 80(R15)
	FMOVD	F15, 88(R15)

	// Initialize Go ABI environment.
	BL	runtime·load_g(SB)

	MOVD	R2, 8(R15)	// fn unsafe.Pointer
	MOVD	R3, 16(R15)	// a unsafe.Pointer
	// Skip R4 = n uint32
	MOVD	R5, 24(R15)	// ctxt uintptr
	BL	runtime·cgocallback(SB)

	FMOVD	32(R15), F8
	FMOVD	40(R15), F9
	FMOVD	48(R15), F10
	FMOVD	56(R15), F11
	FMOVD	64(R15), F12
	FMOVD	72(R15), F13
	FMOVD	80(R15), F14
	FMOVD	88(R15), F15

	// De-allocate stack frame.
	MOVD	$96(R15), R15

	// Restore R6-R15.
	LMG	48(R15), R6, R15

	RET

