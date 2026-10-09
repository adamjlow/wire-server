// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import "errors"

type storageLocation string

const (
	storageCassandra storageLocation = "cassandra"
	storagePostgres  storageLocation = "postgresql"
	storageMigrating storageLocation = "migration-to-postgresql"
)

type postgresMigrationConfig struct {
	Conversation  storageLocation `yaml:"conversation"`
	Codes         storageLocation `yaml:"conversationCodes"`
	TeamFeatures  storageLocation `yaml:"teamFeatures"`
	Domains       storageLocation `yaml:"domainRegistration"`
	User          storageLocation `yaml:"user"`
	BlockList     storageLocation `yaml:"blockList"`
	PasswordReset storageLocation `yaml:"passwordReset"`
}

func (c postgresMigrationConfig) validate() error {
	for _, location := range []storageLocation{c.Conversation, c.Codes, c.TeamFeatures, c.Domains, c.User, c.BlockList, c.PasswordReset} {
		switch location {
		case storageCassandra, storagePostgres, storageMigrating:
		default:
			return errors.New("invalid or missing PostgreSQL migration storage location")
		}
	}
	return nil
}
