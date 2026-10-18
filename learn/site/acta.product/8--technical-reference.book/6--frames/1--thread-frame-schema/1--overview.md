---
title: "Thread Frames"
description: >-
  Overview and scope of thread frames in the Acta implementation.
---

Thread frames are the normalized records used to persist and render provider
conversations. The JSON schema defines their structural shape; the invariants
here describe ordering, identity, and lifecycle behavior.

The downloadable inputs are:

- [Thread frame schema](~attachments/acta-thread-frame-schema)
- [Synthetic frame examples](~attachments/acta-thread-frame-examples)

The examples validate independently; they are not an ordered conversation
stream. The schema uses JSON Schema Draft 2020-12 with shared definitions.
