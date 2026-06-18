
# Action Items

## Bugs

- [ ] Harvey can process PDFs, but the language model doesn't know it can read the PDFs and prompts me to convert it

## Next Steps

- [ ] Make sure assay cli can be used to evaluate the Llamafile models as well as Ollama models.
- [ ] The memory profile commands improvements
  - [ ] don't follow the patter of new, list, use. Example 'show' funcitons like 'list' in the other commands, 'use' is functioning like 'new' in the other commands
  - [ ] The "show" command should show the current active memory profile document
  - [ ] There needs to be an `/memory profile edit`, that let's you edit the current profile, then `/memory profile use <PROFILE_NAME>` could be used to refresh the working memory profile
- [ ] We need a generalized web developer template that reflects the knowledge of Go, uv + Python, SQL (SQLite3 and Postgres), Deno+TypeScript, JavaScript, CSS and HTML5
- [ ] `-resume` option that resumes the most recent session 

## Improvemet Ideas

- [ ] The assay tool for doing model and RAG analysis leaves artifacts in `testout`, This gets confused by other language models as stale error test results. Maybe the model analysis should be stored at the workspace level not in the harvey code repository?
- [ ] Improving Harvey's ergonomics and user experience
  - [ ] Improve status messages to let user informed of what is happening
    - example the debug log type events could be change the spinner behavior to let the user know things are chugging long
    - Harvey. Claude Code and Vibes do this I think people will expect Harvey to do this too.
  - [ ] Command tab complection needs to be better implemented to save user typing.
  - [x] `/status` should show which memory profile is active  ← Phase E of profile-templates-plan.md
  - [x] `/profile` alias for `/memory profile` + `/profile use` for switching  ← Phase C of profile-templates-plan.md
  - [x] Profile templates shipped embedded in binary; template picker replaces blank onboarding  ← profile-templates-plan.md
  - [x] Help guides for Ollama and PDF tools embedded in binary (`/help ollama`, `/help pdf-tools`)  ← Phase D of profile-templates-plan.md
  - [ ] We have the three tiered memory setup which I think it right for Harvey but I think users will think of memory and rag
        settings as going together in many cases. It would be nice to explore that in a way we did with using `/recall` as an alias for `/memory recall`
  - [ ] It would be nice to beable to rename the workspace showing with `/memory profile`
- [ ] Min.io has been taken closed source, evalaute Go module alternatives for support S3 protocol services.
