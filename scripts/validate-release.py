"""Validate release inputs and save notes before any publication steps."""

import os
import re
from pathlib import Path

version = os.environ["VERSION"]
if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?", version):
    raise SystemExit("Invalid release version")
if not os.environ["PREVIOUS"] and version != "v0.1.0-rc.1":
    raise SystemExit(
        "A previous compatible release is required; only v0.1.0-rc.1 can bootstrap layout 2"
    )
if not os.environ["SIGNING_SEED"]:
    raise SystemExit("Configure ACTA_RELEASE_SIGNING_SEED first")
Path("/tmp/release-notes").write_text(os.environ["RELEASE_NOTES"])
