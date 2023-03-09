// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"
#include "abi_amd64.h"

// Set x_crosscall2 point to crosscall2.
// It's such a pointer chain: _crosscall2 -> x_crosscall2 -> crosscall2
TEXT ·set_crosscall2(SB),NOSPLIT,$0-0
    MOVQ	_crosscall2(SB), AX
	MOVQ	$crosscall2(SB), BX
	MOVQ	BX, (AX)
	RET

// Called by C code generated by cmd/cgo.
// func crosscall2(fn, a unsafe.Pointer, n int32, ctxt uintptr)
// Saves C callee-saved registers and calls cgocallback with three arguments.
// fn is the PC of a func(a unsafe.Pointer) function.
// This signature is known to SWIG, so we can't change it.
TEXT crosscall2(SB),NOSPLIT|NOFRAME,$0-0
	PUSH_REGS_HOST_TO_ABI0()

	// Make room for arguments to cgocallback.
	ADJSP	$0x18
#ifndef GOOS_windows
	MOVQ	DI, 0x0(SP)	/* fn */
	MOVQ	SI, 0x8(SP)	/* arg */
	// Skip n in DX.
	MOVQ	CX, 0x10(SP)	/* ctxt */
#else
	MOVQ	CX, 0x0(SP)	/* fn */
	MOVQ	DX, 0x8(SP)	/* arg */
	// Skip n in R8.
	MOVQ	R9, 0x10(SP)	/* ctxt */
#endif

	CALL	runtime·cgocallback(SB)

	ADJSP	$-0x18
	POP_REGS_HOST_TO_ABI0()
	RET
