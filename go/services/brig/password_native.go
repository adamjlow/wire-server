// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux && cgo

package brig

/*
#cgo linux LDFLAGS: -largon2 -lcrypto
#include "native-crypto/argon2.h"

// OpenSSL's stable EVP API; no OpenSSL SDK installation is needed to declare
// this one function. Runtime libraries are explicit candidate image inputs.
extern int EVP_PBE_scrypt(const char *pass, size_t passlen,
    const unsigned char *salt, size_t saltlen,
    uint64_t N, uint64_t r, uint64_t p, uint64_t maxmem,
    unsigned char *key, size_t keylen);

static int brig_argon2(uint8_t *password, uint32_t password_len,
    uint8_t *salt, uint32_t salt_len, uint8_t *out,
    uint32_t memory, uint32_t iterations, uint32_t lanes,
    uint32_t version, int type) {
    argon2_context context = {0};
    context.out = out;
    context.outlen = 16;
    context.pwd = password;
    context.pwdlen = password_len;
    context.salt = salt;
    context.saltlen = salt_len;
    context.t_cost = iterations;
    context.m_cost = memory;
    context.lanes = lanes;
    context.threads = 1;
    context.version = version;
    return argon2_ctx(&context, (argon2_type)type);
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// Both APIs borrow buffers synchronously. C's context is on its stack, contains
// no retained pointers, and uses no callbacks. Its working memory is managed
// and cleared by the library. Errors never expose input or native error text.
func (h passwordHash) deriveNative(plain string) ([]byte, error) {
	password := []byte(plain)
	defer clear(password)
	if h.algorithm == "scrypt" {
		key := make([]byte, 64)
		ok := C.EVP_PBE_scrypt((*C.char)(unsafe.Pointer(unsafe.SliceData(password))), C.size_t(len(password)),
			(*C.uchar)(unsafe.Pointer(unsafe.SliceData(h.salt))), C.size_t(len(h.salt)),
			C.uint64_t(uint64(1)<<h.rounds), C.uint64_t(h.blockSize), C.uint64_t(h.parallelism), C.uint64_t(^uint64(0)),
			(*C.uchar)(unsafe.Pointer(unsafe.SliceData(key))), C.size_t(len(key)))
		runtime.KeepAlive(h)
		if ok != 1 {
			clear(key)
			return nil, errPasswordParameters
		}
		return key, nil
	}
	var variant C.int
	switch h.algorithm {
	case "argon2d":
		variant = C.int(C.Argon2_d)
	case "argon2i":
		variant = C.int(C.Argon2_i)
	case "argon2id":
		variant = C.int(C.Argon2_id)
	default:
		return nil, errPasswordEncoding
	}
	if uint64(len(password)) > 1<<32-1 || uint64(len(h.salt)) > 1<<32-1 {
		return nil, errPasswordParameters
	}
	key := make([]byte, 16)
	ok := C.brig_argon2((*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(password))), C.uint32_t(len(password)),
		(*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(h.salt))), C.uint32_t(len(h.salt)),
		(*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(key))), C.uint32_t(h.memory), C.uint32_t(h.iterations),
		C.uint32_t(h.parallelism), C.uint32_t(h.version), variant)
	runtime.KeepAlive(h)
	if ok != C.ARGON2_OK {
		clear(key)
		return nil, errPasswordParameters
	}
	return key, nil
}
