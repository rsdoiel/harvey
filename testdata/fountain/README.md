# Golden Fountain files

Each `*.golden` file is the Fountain text that a scripted session produces, with the
parts that vary between runs normalised (times, the timings in a stats line, the
workspace path; the author is `TESTER`). The tests in `sessionrender_test.go`,
`sessionlog_offer_test.go`, `sessionlog_chunk_test.go`, `sessionlog_route_test.go` and
`sessionlog_skill_test.go` render the session log of each scenario with `RenderFountain`
and compare the result with its golden file.

The files were captured on 2026-10-30 from renderings that had first been proved equal
to the live recorder's output for the same scenario, so they are the recorder's output as
it was before its live writes were removed (plan item L4 in
`agents/projects/harvey/plans/session-log-plan.md`). One is a deliberate exception:
`tool_call.golden` is the rendering, which has the `[[tool: …]]` note the recorder lost
after tool-result compaction.

To change a golden on purpose, run the tests with `UPDATE_GOLDEN=1`; a file is only
written by a test that has not failed. Review the diff: it is the change to the Fountain
format.
