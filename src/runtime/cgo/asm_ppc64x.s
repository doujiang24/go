// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ppc64 || ppc64le
// +build ppc64 ppc64le

#include "textflag.h"
#include "asm_ppc64x.h"

// Called by C code generated by cmd/cgo.
// func crosscall2(fn, a unsafe.Pointer, n int32, ctxt uintptr)
// Saves C callee-saved registers and calls cgocallback with three arguments.
// fn is the PC of a func(a unsafe.Pointer) function.
// The value of R2 is saved on the new stack frame, and not
// the caller's frame due to issue #43228.
TEXT crosscall2(SB),NOSPLIT|NOFRAME,$0
	// Start with standard C stack frame layout and linkage
	MOVD	LR, R0
	MOVD	R0, 16(R1)	// Save LR in caller's frame
	MOVW	CR, R0		// Save CR in caller's frame
	MOVW	R0, 8(R1)

	BL	runtime·saveregs2(SB)

	MOVDU	R1, (-288-3*8-FIXED_FRAME)(R1)
	// Save the caller's R2
	MOVD	R2, 24(R1)

	// Initialize Go ABI environment
	BL	runtime·reginit(SB)
	BL	runtime·load_g(SB)

#ifdef GOARCH_ppc64
	// ppc64 use elf ABI v1. we must get the real entry address from
	// first slot of the function descriptor before call.
	// Same for AIX.
	MOVD	8(R3), R2
	MOVD	(R3), R3
#endif
	MOVD	R3, FIXED_FRAME+0(R1)	// fn unsafe.Pointer
	MOVD	R4, FIXED_FRAME+8(R1)	// a unsafe.Pointer
	// Skip R5 = n uint32
	MOVD	R6, FIXED_FRAME+16(R1)	// ctxt uintptr
	BL	runtime·cgocallback(SB)

	// Restore the caller's R2
	MOVD	24(R1), R2
	ADD	$(288+3*8+FIXED_FRAME), R1

	BL	runtime·restoreregs2(SB)

	MOVW	8(R1), R0
	MOVFL	R0, $0xff
	MOVD	16(R1), R0
	MOVD	R0, LR
	RET
