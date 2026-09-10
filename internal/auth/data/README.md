# Common password data

Derived from SecLists `Passwords/Common-Credentials/xato-net-10-million-passwords-100000.txt`
at commit `c205c36a445bff37f8e58a9ec829105cd4975c58`.

Source: https://github.com/danielmiessler/SecLists/blob/c205c36a445bff37f8e58a9ec829105cd4975c58/Passwords/Common-Credentials/xato-net-10-million-passwords-100000.txt

MIT license: see LICENSE-SecLists.txt (also embedded in the executable).

Source SHA-256: `1472aafa2561df5e3293aee252aee3ca660c12b399a283cf808bb01b39be388b`.

Transformation: UTF-8 lines, Unicode NFC, trim for blocklist comparison, retain
15–128 code-point entries, lowercase, deduplicate and sort. This retains
70 entries relevant to Acta's minimum password length. The application also
rejects a small supplementary list and single-character repetitions. Passwords
are never sent to an external service. This is a common-password baseline, not
an exhaustive breached-password check. Revisit the list as password policy evolves.
