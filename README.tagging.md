# NUMA Resources operator git tags

We use the git tags to mark milestones in the development:

1. clearer history of the project for future reference
2. make releases and auditing releases easier
3. make integration in other project easier

Unfortunately, we initially tried few models with unsatisfactory results;
tags however should never be removed once published, so these initial attempts
are going to stay there.
Their impact will be diluted over time by the sequence of good tags.

## Tagging model

The tagging model described here is effective from the time 4.11.0 is GA.

We have two tag categories:

1. internal milestone tags
2. external milestone tags

### Internal milestone tags

Here "internal" refers to milestone. Tags are always public (any tag should be consumable;
no tag will be removed).
An "internal" milestone is a significant milestone which brings the project forward, but
not significant enough to trigger a new release; many internal milestones are included in
a release, which we can define then as "major" or "public" milestone.

Internal milestone tags can be created on any branch.
They always have the form `v0.MAJOR.MINOR-0.YYYYMMDDXX` where XX is a sequential number
which starts from `01` and resets when the day changes. This is used to distinguish tags
if we create more than 1 the same solar day. Examples:

```bash
v0.4.10-0.2022030101
v0.4.10-0.2022031501
v0.4.10-0.2022042601
v0.4.10-0.2022042602
```

### External milestone tags

These tags mark release candidates, for major milestones (releases).
The only novelty factor is we create tags in pairs, because the major version we use
conflicts with the project layout go packages would expect for a version >= 1.
This decision is likely to be revisited in the future.

External milestone / release candidate tags are:

- **always done in pairs referring to the same commit**.
- **always done on a release branch, never on the main branch** (there could be some odd ones due
  to historical reasons, see introduction)

The form of a release candidate branch is:
`vMAJOR.MINOR.PATCH-rcX` where X is a sequentially increasing integer. We will *ALWAYS* have `rc0`.
We CAN have more RCs, like `rc1`, `rc2`.
Examples:

```bash
v4.11.4-rc0
v4.11.7-rc0
v4.11.7-rc1
v4.12.0-rc0
v4.12.1-rc0
```

Each release candidate tag also have a mirror, golang-friendlier alias tag (**referring to the very same commit**)
with the form `v0.MAJOR.MINOR-PATCHrcX`. Examples:

```bash
v0.4.11-4rc0
v0.4.11-7rc0
v0.4.11-7rc1
v0.4.12-0rc0
v0.4.12-1rc0
```
