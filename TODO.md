

# Improvemet Ideas

- [ ] `-resume` option that resumes the most recent session 
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

