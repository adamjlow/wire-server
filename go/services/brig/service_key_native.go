// SPDX-License-Identifier: AGPL-3.0-only
//go:build linux && cgo

package brig

/*
#include <stdio.h>
#include <openssl/bn.h>
#include <openssl/bio.h>
#include <openssl/evp.h>
#include <openssl/pem.h>
#include <openssl/core_names.h>

static int brig_service_key(const void *pem, int length, int *bits, unsigned char *fingerprint) {
    int ok = 0;
    BIO *bio = BIO_new_mem_buf(pem, length);
    EVP_PKEY *key = NULL;
    BIGNUM *n = NULL, *e = NULL;
    EVP_MD_CTX *digest = NULL;
    unsigned char *mpi = NULL;
    if (bio == NULL) goto done;
    key = PEM_read_bio_PUBKEY(bio, NULL, NULL, NULL);
    if (key == NULL || EVP_PKEY_base_id(key) != EVP_PKEY_RSA) goto done;
    int size = EVP_PKEY_get_size(key);
    if (size < 256 || size > 268435455) goto done;
    if (!EVP_PKEY_get_bn_param(key, OSSL_PKEY_PARAM_RSA_N, &n) ||
        !EVP_PKEY_get_bn_param(key, OSSL_PKEY_PARAM_RSA_E, &e)) goto done;
    digest = EVP_MD_CTX_new();
    if (digest == NULL || !EVP_DigestInit_ex(digest, EVP_sha256(), NULL)) goto done;
    char decimal[32];
    int count = snprintf(decimal, sizeof(decimal), "%d", size);
    if (count < 1 || (size_t)count >= sizeof(decimal) || !EVP_DigestUpdate(digest, decimal, count)) goto done;
    BIGNUM *values[2] = {n,e};
    for (int i = 0; i < 2; i++) {
        int length = BN_bn2mpi(values[i], NULL);
        if (length < 4) goto done;
        mpi = OPENSSL_malloc(length);
        if (mpi == NULL || BN_bn2mpi(values[i], mpi) != length || !EVP_DigestUpdate(digest, mpi, length)) goto done;
        OPENSSL_free(mpi); mpi = NULL;
    }
    unsigned int output_length = 0;
    if (!EVP_DigestFinal_ex(digest, fingerprint, &output_length) || output_length != 32) goto done;
    *bits = size * 8;
    ok = 1;
done:
    OPENSSL_free(mpi);
    EVP_MD_CTX_free(digest);
    BN_free(n); BN_free(e);
    EVP_PKEY_free(key); BIO_free(bio);
    return ok;
}
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"
)

var errServiceKeyInvalid = errors.New("invalid service key")

func (p *passwordService) serviceKey(ctx context.Context, encoded []byte) (int32, []byte, error) {
	if len(encoded) == 0 || uint64(len(encoded)) > 2147483647 {
		return 0, nil, errServiceKeyInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case p.slots <- struct{}{}:
	}
	defer func() { <-p.slots }()
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	var size C.int
	fingerprint := make([]byte, 32)
	ok := C.brig_service_key(unsafe.Pointer(&encoded[0]), C.int(len(encoded)), &size, (*C.uchar)(unsafe.Pointer(&fingerprint[0])))
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	if ok != 1 {
		return 0, nil, errServiceKeyInvalid
	}
	return int32(size), fingerprint, nil
}
