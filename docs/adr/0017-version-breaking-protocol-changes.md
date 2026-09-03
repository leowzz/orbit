---
status: accepted
---

# Version breaking protocol changes

The `orbit.v1` package and `orbit/v1/...` Topics may evolve only through changes that old consumers can ignore or safely degrade, such as optional fields and new enum values. Removing fields, changing existing semantics, or changing Topic routing creates a new Protobuf package and Topic version. Core subscribes to `orbit/#` but ignores protocol versions it does not implement.
