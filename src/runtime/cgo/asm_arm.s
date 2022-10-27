// Copyright 2012 The Go Authors. All rights reserved.
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
	SUB	$(8*9), R13 // Reserve space for the floating point registers.
	// The C arguments arrive in R0, R1, R2, and R3. We want to
	// pass R0, R1, and R3 to Go, so we push those on the stack.
	// Also, save C callee-save registers R4-R12.
	MOVM.WP	[R0, R1, R3, R4, R5, R6, R7, R8, R9, g, R11, R12], (R13)
	// Finally, save the link register R14. This also puts the
	// arguments we pushed for cgocallback where they need to be,
	// starting at 4(R13).
	MOVW.W	R14, -4(R13)

	// Skip floating point registers on GOARM < 6.
	MOVB    runtime·goarm(SB), R11
	CMP $6, R11
	BLT skipfpsave
	MOVD	F8, (13*4+8*1)(R13)
	MOVD	F9, (13*4+8*2)(R13)
	MOVD	F10, (13*4+8*3)(R13)
	MOVD	F11, (13*4+8*4)(R13)
	MOVD	F12, (13*4+8*5)(R13)
	MOVD	F13, (13*4+8*6)(R13)
	MOVD	F14, (13*4+8*7)(R13)
	MOVD	F15, (13*4+8*8)(R13)

skipfpsave:
	BL	runtime·load_g(SB)
	// We set up the arguments to cgocallback when saving registers above.
	BL	runtime·cgocallback(SB)

	MOVB    runtime·goarm(SB), R11
	CMP $6, R11
	BLT skipfprest
	MOVD	(13*4+8*1)(R13), F8
	MOVD	(13*4+8*2)(R13), F9
	MOVD	(13*4+8*3)(R13), F10
	MOVD	(13*4+8*4)(R13), F11
	MOVD	(13*4+8*5)(R13), F12
	MOVD	(13*4+8*6)(R13), F13
	MOVD	(13*4+8*7)(R13), F14
	MOVD	(13*4+8*8)(R13), F15

skipfprest:
	MOVW.P	4(R13), R14
	MOVM.IAW	(R13), [R0, R1, R3, R4, R5, R6, R7, R8, R9, g, R11, R12]
	ADD	$(8*9), R13
	MOVW	R14, R15
