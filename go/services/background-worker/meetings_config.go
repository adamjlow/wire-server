// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type MeetingsCleanupConfig struct {
	RetentionHours float64 `yaml:"cleanOlderThanHours"`
	BatchSize      int     `yaml:"batchSize"`
	Schedule       string  `yaml:"schedule"`
}

func (c MeetingsCleanupConfig) validate() error {
	if math.IsNaN(c.RetentionHours) || math.IsInf(c.RetentionHours, 0) || c.RetentionHours < 0 || c.BatchSize <= 0 {
		return errors.New("invalid meetings cleanup configuration")
	}
	_, err := parseScheduledCron(c.Schedule)
	return err
}
func (c *MeetingsCleanupConfig) UnmarshalYAML(node *yaml.Node) error {
	invalid := errors.New("invalid meetings cleanup configuration")
	if node.Kind != yaml.MappingNode {
		return invalid
	}
	var raw struct {
		RetentionHours *float64 `yaml:"cleanOlderThanHours"`
		BatchSize      *int     `yaml:"batchSize"`
		Schedule       *string  `yaml:"schedule"`
	}
	if node.Decode(&raw) != nil || raw.RetentionHours == nil || raw.BatchSize == nil || raw.Schedule == nil {
		return invalid
	}
	// Native Aeson configuration requires numbers and Text, not YAML decoder
	// coercions of quoted numbers or numeric scalars to strings.
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if value.Kind == yaml.AliasNode {
			if value.Alias == nil {
				return invalid
			}
			value = value.Alias
		}
		switch key {
		case "cleanOlderThanHours", "batchSize":
			if value.Tag != "!!int" && value.Tag != "!!float" {
				return invalid
			}
			if key == "batchSize" {
				var n float64
				if value.Decode(&n) != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
					return invalid
				}
			}
		case "schedule":
			if value.Tag != "!!str" {
				return invalid
			}
		}
	}
	decoded := MeetingsCleanupConfig{RetentionHours: *raw.RetentionHours, BatchSize: *raw.BatchSize, Schedule: *raw.Schedule}
	if err := decoded.validate(); err != nil {
		return err
	}
	decoded.Schedule = canonicalCronExpression(decoded.Schedule)
	*c = decoded
	return nil
}

// Options parses CronSchedule then Workers serializes it before cron registration.
// Match native ShowT: macros expand, numeric/name fields become decimal, while
// ranges, list order/duplicates and star/step structure remain intact.
func canonicalCronExpression(expression string) string {
	expression = strings.ToLower(expression)
	macros := map[string]string{"@yearly": "0 0 1 1 *", "@monthly": "0 0 1 * *", "@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@hourly": "0 * * * *"}
	if expanded, ok := macros[expression]; ok {
		return expanded
	}
	fields := strings.Split(expression, " ")
	for index, field := range fields {
		names := ""
		if index == 3 {
			names = "jan feb mar apr may jun jul aug sep oct nov dec"
		}
		if index == 4 {
			names = "mon tue wed thu fri sat sun"
		}
		for n, name := range strings.Fields(names) {
			field = strings.ReplaceAll(field, name, strconv.Itoa(n+1))
		}
		// All characters are known valid after parseScheduledCron. Canonicalize
		// each numeric run without rewriting separators or the AST's structure.
		var out strings.Builder
		for i := 0; i < len(field); {
			if field[i] < '0' || field[i] > '9' {
				out.WriteByte(field[i])
				i++
				continue
			}
			end := i + 1
			for end < len(field) && field[end] >= '0' && field[end] <= '9' {
				end++
			}
			number, _ := strconv.Atoi(field[i:end])
			out.WriteString(strconv.Itoa(number))
			i = end
		}
		fields[index] = out.String()
	}
	return strings.Join(fields, " ")
}

// Source converts Double hours to fixed Pico before multiplying by3600. Keep
// arbitrary-integer precision, then native PostgreSQL.Binary.Time's floor to
// microseconds and Int64 wire conversion. Extreme finite retentions can wrap the
// timestamp in that encoder; preserve it and record the inherited concern.
func meetingRetentionCutoff(now time.Time, hours float64) (time.Time, error) {
	if math.IsNaN(hours) || math.IsInf(hours, 0) || hours < 0 {
		return time.Time{}, errors.New("invalid meeting retention")
	}
	rational := new(big.Rat).SetFloat64(hours)
	scaled := new(big.Int).Mul(rational.Num(), big.NewInt(1000000000000))
	picoHours := new(big.Int).Quo(scaled, rational.Denom())
	retention := new(big.Int).Mul(picoHours, big.NewInt(3600))
	// PostgreSQL epoch is 2000; source UTC day arithmetic ignores leap seconds.
	nowPico := new(big.Int).Sub(big.NewInt(now.Unix()), big.NewInt(946684800))
	nowPico.Mul(nowPico, big.NewInt(1000000000000))
	nowPico.Add(nowPico, big.NewInt(int64(now.Nanosecond())*1000))
	cutoffPico := new(big.Int).Sub(nowPico, retention)
	micro := new(big.Int).Div(cutoffPico, big.NewInt(1000000))
	modulus := new(big.Int).Lsh(big.NewInt(1), 64)
	wrapped := new(big.Int).Mod(micro, modulus)
	if wrapped.Bit(63) != 0 {
		wrapped.Sub(wrapped, modulus)
	}
	value := wrapped.Int64()
	// Normalize the encoded value to a Go UTC time. pgx reproduces the same
	// Int64 wire bytes, including native infinity sentinels, without Duration.
	return time.Unix(value/1000000+946684800, (value%1000000)*1000).UTC(), nil
}
