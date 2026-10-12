// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

type servicePEMError struct{ message string }

func (e servicePEMError) Error() string { return e.message }

// Data.PEM decodes each content line separately and does not parse headers.
// Go's PEM reader accepts more forms; keep the native parser boundary here.
func nativeServicePEM(text string) ([]byte, error) {
	failure := func(message string) ([]byte, error) { return nil, servicePEMError{message} }
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	var blocks []*pem.Block
	name := func(line, prefix string) (string, bool) {
		value := strings.TrimPrefix(line, prefix)
		index := strings.IndexByte(value, '-')
		if index < 0 || value[index:] != "-----" {
			return "", false
		}
		return value[:index], true
	}
	for index := 0; index < len(lines); index++ {
		if !strings.HasPrefix(lines[index], "-----BEGIN ") {
			continue
		}
		label, valid := name(lines[index], "-----BEGIN ")
		if !valid {
			return failure("invalid PEM delimiter found")
		}
		block := &pem.Block{Type: label}
		closed := false
		for index++; index < len(lines); index++ {
			line := lines[index]
			if strings.HasPrefix(line, "-----END ") {
				end, valid := name(line, "-----END ")
				if !valid {
					return failure("invalid PEM delimiter found")
				}
				if end != label {
					return failure("invalid PEM: end name doesn't match start name")
				}
				closed = true
				break
			}
			if len(line)%4 != 0 {
				return failure("invalid PEM: decoding failed: Base64-encoded bytestring requires padding")
			}
			content, err := base64.StdEncoding.DecodeString(line)
			if err != nil {
				return failure("invalid PEM: decoding failed: invalid base64 encoding")
			}
			block.Bytes = append(block.Bytes, content...)
		}
		if !closed {
			return failure("invalid PEM: no end marker found")
		}
		blocks = append(blocks, block)
	}
	if len(blocks) != 1 {
		return failure("Too many sections in PEM format. Expected 1.")
	}
	if len(blocks[0].Bytes) == 0 {
		return []byte(fmt.Sprintf("-----BEGIN %s-----\n\n-----END %s-----\n", blocks[0].Type, blocks[0].Type)), nil
	}
	return pem.EncodeToMemory(blocks[0]), nil
}
