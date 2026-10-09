// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"math"
	"unicode/utf8"
)

type cassandraSubConversation struct {
	ID                    string
	GroupID               []byte
	CipherSuite           int32
	Epoch, EpochWriteTime int64
}

func decodeCassandraSubConversation(row map[string]any) (cassandraSubConversation, error) {
	var sub cassandraSubConversation
	var ok bool
	sub.ID, ok = row["subconv_id"].(string)
	if !ok || !utf8.ValidString(sub.ID) {
		return sub, errors.New("invalid stored subconversation")
	}
	sub.GroupID, ok = row["group_id"].([]byte)
	if !ok || sub.GroupID == nil {
		return sub, errors.New("invalid stored subconversation group")
	}
	cs, valid := cqlInteger(row["cipher_suite"])
	if !valid || cs < math.MinInt32 || cs > math.MaxInt32 {
		return sub, errors.New("invalid stored subconversation cipher")
	}
	switch uint16(cs) {
	case 1, 2, 5, 7:
	default:
		return sub, errors.New("invalid stored subconversation cipher")
	}
	sub.CipherSuite = int32(uint16(cs))
	sub.Epoch, ok = cqlInteger(row["epoch"])
	if !ok {
		return sub, errors.New("invalid stored subconversation epoch")
	}
	sub.EpochWriteTime, ok = cqlInteger(row["writetime(epoch)"])
	if !ok {
		return sub, errors.New("invalid stored subconversation epoch timestamp")
	}
	return sub, nil
}
