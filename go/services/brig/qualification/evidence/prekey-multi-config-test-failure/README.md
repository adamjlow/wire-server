# Retained candidate configuration-test fixture failure

Sourced5a2d11df SDK checks failed in TestPrekeyMapDecodeNormalizationAndLimits:
new candidate test used settings instead of native optSettings root YAML key,
so decoded setMaxConvSize was nil. Correct the candidate fixture key only;
retain Word16 bounds/presence assertions. This is not an original oracle test.
SDK exit1/network removal0 and managed container/network empty. No service
build/backend/original execution claimed for this failed source.
