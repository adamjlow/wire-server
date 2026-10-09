// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func cassandraMigrationPage(session *gocql.Session, cql string, columns ...string) migrationPage {
	return func(ctx context.Context, state []byte, size int) ([]migrationRow, []byte, error) {
		iter := session.Query(cql).WithContext(ctx).Consistency(gocql.LocalQuorum).
			RetryPolicy(cassandraRetry(5)).PageSize(size).PageState(state).Iter()
		var rows []migrationRow
		row := nullableCassandraRow(columns...)
		for iter.MapScan(row) {
			unwrapCassandraRow(row)
			rows = append(rows, migrationRow(row))
			row = nullableCassandraRow(columns...)
		}
		next := append([]byte(nil), iter.PageState()...)
		if err := iter.Close(); err != nil {
			return nil, nil, errors.New("Cassandra migration page failed")
		}
		return rows, next, nil
	}
}

func validatedMigrationPage(page migrationPage, validate func(migrationRow) error) migrationPage {
	return func(ctx context.Context, state []byte, size int) ([]migrationRow, []byte, error) {
		rows, next, err := page(ctx, state, size)
		if err != nil {
			return nil, nil, err
		}
		// Native typed pagination decodes the entire page before yielding any
		// rows to writes. A malformed later row must not permit earlier copies.
		for _, row := range rows {
			if err := validate(row); err != nil {
				return nil, nil, err
			}
		}
		return rows, next, nil
	}
}
