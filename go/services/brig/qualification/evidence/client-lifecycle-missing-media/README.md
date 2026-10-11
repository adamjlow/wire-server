# Retained missing-client media failure

Sourcee39a1005e, unchanged service4447e62d. Seven native/Go admission
responses matched; native read identical Go-created client/MLS/prekey state.
Go deletion removed the native client and prekeys, confirmed on native restart.
Overall run FAILED because missing-client GET404 returned no Content-Type in
Go versus native text/plain;charset=utf-8. No oracle/assertion change or waiver.
Native prekey claim/recreated-client MLS-retention witnesses were not reached.
All13 managed processes exit0/runner1/cleanup0; independent project audit empty.
The subsequent candidate media fix requires a fresh exact-executable run.
