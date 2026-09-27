//go:build windows && gui

package main

/*
#cgo windows LDFLAGS: -limm32 -luser32
#include <stdint.h>
#include <windows.h>
#include <imm.h>

static void hd2_set_ime_position(uintptr_t hwndValue, int x, int y, int lineHeight) {
	HWND hwnd = (HWND)(uintptr_t)hwndValue;
	if (hwnd == NULL) {
		return;
	}
	HIMC context = ImmGetContext(hwnd);
	if (context == NULL) {
		return;
	}
	COMPOSITIONFORM form;
	ZeroMemory(&form, sizeof(form));
	form.dwStyle = CFS_FORCE_POSITION;
	form.ptCurrentPos.x = x;
	form.ptCurrentPos.y = y + lineHeight;
	ImmSetCompositionWindow(context, &form);
	CANDIDATEFORM candidate;
	ZeroMemory(&candidate, sizeof(candidate));
	candidate.dwStyle = CFS_CANDIDATEPOS;
	candidate.ptCurrentPos.x = x;
	candidate.ptCurrentPos.y = y + lineHeight;
	ImmSetCandidateWindow(context, &candidate);
	ImmReleaseContext(hwnd, context);
}
*/
import "C"

func setIMEPosition(hwnd uintptr, x, y, lineHeight int) {
	C.hd2_set_ime_position(C.uintptr_t(hwnd), C.int(x), C.int(y), C.int(lineHeight))
}
