---
id: "0002"
title: "Freeze DECISIONS.md; new decisions are kb records in harvey/decisions"
date: "2026-09-23"
status: accepted
kind: decision
trigger: design
project: harvey
phase: ""
supersedes: []
superseded_by: []
relates_to: []
initiative: ""
session: ""
decisions: []
tags: []
uuid: "01a0d021-191e-7e06-99e6-b0f727c1bbc0"
origin_host: "wren"
---

**Context.**

Root `TODO.md` asks harvey to adopt the decision-record practice `kb`
supports. `harvey/DECISIONS.md` is a single hand-appended file of 1,721
lines. `harvey/decisions/` did not exist before this record.

**Decision.**

Freeze `DECISIONS.md` as history. New decisions are authored with
`kb record new --project harvey --dir harvey/decisions`, and indexed with
`kb ingest` and `kb index`. No bulk backfill; a still-load-bearing older
decision is converted on demand.

**Rationale.**

Converting 1,721 lines is large and mostly archival. Freezing gives one
clean cutover point and lets `kb search` reach every decision made from now
on.

**Rejected alternatives.**

- Full backfill: large, low value, risks mis-transcribing intent.
- Keep appending to `DECISIONS.md` alongside records: two sources of truth.

**Consequences.**

Decisions before this record are outside `knowledge.db`. Status `proposed`:
promotion is the author's call.
