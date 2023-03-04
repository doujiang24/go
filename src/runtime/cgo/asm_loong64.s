// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"

// Set the x_crosscall2 function pointer variable in C point to crosscall2.
// It's such a pointer chain: _crosscall2 -> x_crosscall2 -> crosscall2
TEXT ·set_crosscall2(SB),NOSPLIT,$0-0
	MOVV	_crosscall2(SB), R5
	MOVV	$crosscall2(SB), R6
	MOVV	R6, (R5)
	RET

// Called by C code generated by cmd/cgo.
// func crosscall2(fn, a unsafe.Pointer, n int32, ctxt uintptr)
// Saves C callee-saved registers and calls cgocallback with three arguments.
// fn is the PC of a func(a unsafe.Pointer) function.
TEXT crosscall2(SB),NOSPLIT|NOFRAME,$0
	/*
	 * We still need to save all callee save register as before, and then
	 * push 3 args for fn (R4, R5, R7), skipping R6.
	 * Also note that at procedure entry in gc world, 8(R29) will be the
	 *  first arg.
	 */

	ADDV	$(-8*22), R3
	MOVV	R4, (8*1)(R3) // fn unsafe.Pointer
	MOVV	R5, (8*2)(R3) // a unsafe.Pointer
	MOVV	R7, (8*3)(R3) // ctxt uintptr
	MOVV	R23, (8*4)(R3)
	MOVV	R24, (8*5)(R3)
	MOVV	R25, (8*6)(R3)
	MOVV	R26, (8*7)(R3)
	MOVV	R27, (8*8)(R3)
	MOVV	R28, (8*9)(R3)
	MOVV	R29, (8*10)(R3)
	MOVV	R30, (8*11)(R3)
	MOVV	g, (8*12)(R3)
	MOVV	R1, (8*13)(R3)
	MOVD	F24, (8*14)(R3)
	MOVD	F25, (8*15)(R3)
	MOVD	F26, (8*16)(R3)
	MOVD	F27, (8*17)(R3)
	MOVD	F28, (8*18)(R3)
	MOVD	F29, (8*19)(R3)
	MOVD	F30, (8*20)(R3)
	MOVD	F31, (8*21)(R3)

	// Initialize Go ABI environment
	JAL	runtime·load_g(SB)

	JAL	runtime·cgocallback(SB)

	MOVV	(8*4)(R3), R23
	MOVV	(8*5)(R3), R24
	MOVV	(8*6)(R3), R25
	MOVV	(8*7)(R3), R26
	MOVV	(8*8)(R3), R27
	MOVV	(8*9)(R3), R28
	MOVV	(8*10)(R3), R29
	MOVV	(8*11)(R3), R30
	MOVV	(8*12)(R3), g
	MOVV	(8*13)(R3), R1
	MOVD	(8*14)(R3), F24
	MOVD	(8*15)(R3), F25
	MOVD	(8*16)(R3), F26
	MOVD	(8*17)(R3), F27
	MOVD	(8*18)(R3), F28
	MOVD	(8*19)(R3), F29
	MOVD	(8*20)(R3), F30
	MOVD	(8*21)(R3), F31
	ADDV	$(8*22), R3

	RET
