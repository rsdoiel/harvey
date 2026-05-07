
# Llamafile, a language model ready to execute

[Llamafile](https://mozilla-ai.github.io/llamafile/) is a project by Mozilla AI to deliver OS agnostic and hardware agnostic ready to run language models as a single executable file. They have done this by combining llama.cpp and Cosmopolitin c library for a portable delivery format without need to run emulators or a virtual machine. See they [project page](https://github.com/mozilla-ai/llamafile) for more details.

How does this works with Harvey? Well it givens Harvey another way to use the language model of your choice in addition to running Ollama. I suspect this will mean that Harvey could ship with a couple default language models ready to go rather than require you to find and install Ollama for your specific hardware and OS combo.  The Mozilla AI project is now at release v0.10.0, which while still pre-v1.x shows it is steadily developing. One challenge is that while conceptual Llamafiles are pretty compelling finding models already compiled to it is a bit more of a challenge. Fortunately they include examples in their Git repository, you find those [here](https://github.com/mozilla-ai/llamafile/blob/main/docs/example_llamafiles.md). Aside from tracking the evolution of llama.cpp the latest format make its easier to compiler your own models. This is promising of itself.

Where does Harvey go with this? Harvey has already integrated Mozilla AI's any-llm-go module. Harvey has been updated to support Llamafiles though that remains an untested feature. In coming releases of Harvey I am hopefuly that'll change and Llamafiles can become a regular part of Harvey, perhaps it's primary model support.  Still there is allot of research to be digested and code to be tested and refined.


