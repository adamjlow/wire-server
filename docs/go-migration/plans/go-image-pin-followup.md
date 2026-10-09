# Optional image pin follow-up

The security patch changed optional tooling to Go1.26.9; four prior service image
builders hardcode1.26.8, so leaving them unchanged breaks advertised optional
build commands under the updated shell/CI pin. This narrow framework follow-up
updates only their required compiler strings and README build instruction. Keep
runtime source/Go modules/defaults/oracles/historical receipts unchanged. Receipt
validation remains fail-closed, no remote push or production action. Existing
Python builder/selection tests, syntax and scope checks suffice for pin-string
changes; this does not requalify old runtime artifacts. Serious old Go1.26.8
image risks remain recorded pending actual patched rebuild and finite reuse of
original tests, as documented in the security patch review.
