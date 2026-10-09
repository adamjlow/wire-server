Migration counter output contract correction

Initial original MLS execution in ../conversation-original-initial-20261009
reached enabled migrations then failed the native wait helper: it matches exactly
1.0/0.0, while Go printed integer1/0. Original Test/Migration/Util.hs was read and
left unchanged. Candidate migrated/finished/failed counters now emit decimal.0;
focused counter expectations and pinned Go1.26.9 full checks pass for exact
source-files.json inputs. No assertion/oracle loosening or baseline waiver.
No dependency change; informational scanner/license warnings remain retained.
Next rerun the unchanged original MLS test on a clean exact static executable.
Whole-service/scheduled/final124/switchback qualification remains incomplete.
