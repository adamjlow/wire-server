Actual bounded Haskell-Go-Haskell rehearsal on source94881e1cd: initial native
forced stop137 after5.064s retains attempt1/lease, Go reclaims with different owner
and increased attempt, completes scheduled jobs, meeting/code cleanup and the
explicit partial user-copy continuation, then stops0/0.0497s preserving both
notifications. Return Haskell storage convergence assertion passes but Rabbit
notification delivery fails: fixture200 response lacks Content-Type, native
UnsupportedContentType application/octet-stream retries indefinitely. Go did not
receive that response during its held delivery; its source would accept it.
Controller1/cleanup0/no owned resources. This is not final switchback success.
Fix the candidate's actual used notification response acceptance and the fixture's
valid successful response, then requalify changed executable; don't waive this
concrete ACK/retry contract difference. API primary source requires Post200/JSON
EmptyResponse (any object), not arbitrary2xx/body/media success. Retained logs show
the real native rejection; original oracle/assertions and schemas remain untouched.
