

# Improvemet Ideas

- [ ] The assay tool for doing model and RAG analysis leaves artifacts in `testout`, This gets confused by other language models as stale error test results. Maybe the model analysis should be stored at the workspace level not in the harvey code repository?
- [ ] Improving Harvey's ergonomics and user experience
  - [ ] Improve status messages to let user informed of what is happening
    - example the debug log type events could be change the spinner behavior to let the user know things are chugging long
    - Harvey. Claude Code and Vibes do this I think people will expect Harvey to do this too.
  - [ ] Command tab complection needs to be better implemented to save user typing.
  - [ ] `/status` should show which memory profile is active
  - [ ] `/profile` could be a nice alias for `/memory profile`
  - [ ] We have the three tiered memory setup which I think it right for Harvey but I think users will think of memory and rag
        settings as going together in many cases. It would be nice to explore that in a way we did with using `/recall` as an alias for `/memory recall`
  - [ ] It would be nice to beable to rename the workspace showing with `/memory profile`

