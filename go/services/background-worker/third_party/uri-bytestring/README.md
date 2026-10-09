stored_https_url.go adapts the storage parse/serialize behavior of
uri-bytestring-0.3.3.1 (URI.ByteString.Internal) to preserve the Haskell
stored HttpsUrl contract. Original BSD3 notice is retained here and must
be included in binary/container distributions.

https://hackage.haskell.org/package/uri-bytestring-0.3.3.1
https://hackage-content.haskell.org/package/uri-bytestring-0.3.3.1/src/src/URI/ByteString/Internal.hs

Only storage conversion happens here. No URL is fetched and no password/hash
is logged. Native serialization changes path/query escapes and inherited IPv6
literal serialization removes brackets; migration preserves those source effects.
