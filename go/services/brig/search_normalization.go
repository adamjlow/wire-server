// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux && cgo

package brig

/*
#cgo LDFLAGS: -L/opt/brig-cxx/usr/lib/gcc/x86_64-linux-gnu/14 -l:libicui18n.a -l:libicuuc.a -l:libicudata.a -l:libstdc++.a -lm
#include <unicode/utrans.h>
#include <unicode/ustring.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>

// Every invocation owns its transliterator and buffers: no shared mutable ICU
// object, retained Go pointer or callback crosses this synchronous boundary.
static char *brig_normalize(const UChar *input, int32_t length, int32_t *outlen) {
    static const UChar rule[] = {
      'A','n','y','-','L','a','t','i','n',';',' ',
      'L','a','t','i','n','-','A','S','C','I','I',';',' ',
      'L','o','w','e','r',0
    };
    UErrorCode error = U_ZERO_ERROR;
    UTransliterator *trans = utrans_openU(rule, -1, UTRANS_FORWARD, NULL, 0, NULL, &error);
    if (U_FAILURE(error) || trans == NULL) return NULL;
    UChar *buffer = NULL;
    char *result = NULL;
    int64_t capacity = (int64_t)length + 64;
    for (;;) {
      if (capacity > INT32_MAX) break;
      buffer = (UChar *)malloc((size_t)capacity * sizeof(UChar));
      if (buffer == NULL) break;
      if (length > 0) memcpy(buffer, input, (size_t)length * sizeof(UChar));
      int32_t size = length, limit = length;
      error = U_ZERO_ERROR;
      utrans_transUChars(trans, buffer, &size, (int32_t)capacity, 0, &limit, &error);
      if (error == U_BUFFER_OVERFLOW_ERROR) {
        free(buffer); buffer = NULL;
        capacity = size > capacity ? (int64_t)size + 1 : capacity * 2;
        continue;
      }
      if (U_FAILURE(error)) break;
      error = U_ZERO_ERROR;
      int32_t needed = 0;
      u_strToUTF8(NULL, 0, &needed, buffer, size, &error);
      if (error != U_BUFFER_OVERFLOW_ERROR && U_FAILURE(error)) break;
      if (needed == INT32_MAX) break;
      result = (char *)malloc((size_t)needed + 1);
      if (result == NULL) break;
      error = U_ZERO_ERROR;
      u_strToUTF8(result, needed + 1, outlen, buffer, size, &error);
      if (U_FAILURE(error)) { free(result); result = NULL; }
      break;
    }
    free(buffer);
    utrans_close(trans);
    return result;
}
*/
import "C"

import (
	"errors"
	"math"
	"runtime"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

func normalizedSearchName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", errors.New("invalid search name encoding")
	}
	input := utf16.Encode([]rune(name))
	if len(input) > math.MaxInt32-64 {
		return "", errors.New("search name too large")
	}
	var length C.int32_t
	out := C.brig_normalize((*C.UChar)(unsafe.Pointer(unsafe.SliceData(input))), C.int32_t(len(input)), &length)
	runtime.KeepAlive(input)
	if out == nil {
		return "", errors.New("search name normalization failed")
	}
	defer C.free(unsafe.Pointer(out))
	return C.GoStringN(out, C.int(length)), nil
}
