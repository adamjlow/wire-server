// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import "github.com/apache/cassandra-gocql-driver/v2"

// Default gocql MapScan loses scalar NULLs (UUID zero, status0, name"").
// Native CQL Maybe fields must retain absence separately from present zero.
type nullableCassandraValue struct{ Value any }

func (v *nullableCassandraValue) UnmarshalCQL(info gocql.TypeInfo, data []byte) error {
	if data == nil {
		v.Value = nil
		return nil
	}
	v.Value = info.Zero()
	return gocql.Unmarshal(info, data, &v.Value)
}

func nullableCassandraRow(names ...string) map[string]any {
	row := make(map[string]any, len(names))
	for _, name := range names {
		row[name] = &nullableCassandraValue{}
	}
	return row
}

func unwrapCassandraRow(row map[string]any) {
	for name, value := range row {
		switch v := value.(type) {
		case nullableCassandraValue:
			row[name] = v.Value
		case *nullableCassandraValue:
			row[name] = v.Value
		}
	}
}
