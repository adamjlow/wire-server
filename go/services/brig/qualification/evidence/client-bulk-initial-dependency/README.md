# Retained first client bulk check

Source741d6e5ad231b464646beb3dcdfa81259f305bf5 failed go mod tidy: existing golang.org/x/sync v0.23.0 became directly imported but remained declared indirect. No unit/storage/API execution from this failed attempt. Controller1, cleanup0, owned containers/networks empty. Fixed declaration only at fc0063a69; no new module/version.
