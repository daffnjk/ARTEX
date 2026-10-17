# GOV.0 environment evidence

Observed source: main 674dd067eb4eba7dc95b786a63b7576abb064a48; tree 3421bef6a712b1b668915dcbcb2666ee7fddff2d. GitHub compare main...research/evidence-harness-m1: behind, ahead_by=0, behind_by=1, files=[]. PR #1 merged at that main commit. Review comment 4301953152 remains relevant to evaluation.go blob ac7fa2c09e29c98ccfaa143e1140383c2ce0d1fb.

Local commands executed 2026-10-17 UTC:

```
git --version
# git version 2.47.3
go version
# go version go1.23.2 linux/amd64
git clone https://github.com/daffnjk/ARTEX.git /mnt/data/artex-work/ARTEX
# fatal: unable to access ... Could not resolve host: github.com
sha256sum -c MANIFEST.sha256
# all listed handoff payloads OK
```

No psql/postgres/docker executable or /usr/lib/postgresql or /usr/local/lib/postgresql was found. No existing repository checkout/dirty files were present in the dedicated work area. The unsuccessful clone did not yield a usable checkout. Archive download could not pass the download tool's viewed-URL requirement; no successful archive acquisition is claimed.

These observations do not prove repository tests, PG semantics, UI, target isolation or research performance. Existing GitHub CI and connector access provide a possible acquisition/verification path, not an advance PASS.
