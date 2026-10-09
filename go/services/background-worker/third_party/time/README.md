The adminless timestamp implementation preserves Data.Time formatting and
timezone/leap-second semantics, compared with primary time 1.12.2 source.
UTCTimeMillis uses %FT%T.%03qZ; %Y is unpadded. Local timezone conversion
normalizes hours/minutes separately from seconds, preserving end-of-day leaps.
This attribution is a source comparison, not freshly identified embedded
Haskell package metadata or native decoder execution. Retain the BSD3 notice
in eventual source/binary/container distributions.
https://github.com/haskell/time/tree/1.12.2
