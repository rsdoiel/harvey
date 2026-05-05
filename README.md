

# Harvey

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is an agent REPL. It is written in Go and designed to use Ollama server to access language models. These can be run locally or remotely since Harvey uses Ollama's web service as the integration point. Harvey is a terminal based application. It will run on Raspberry Pi OS (Raspberry Pi 5 hardware), Linux (arm64 and amd64), Windows (arm64, amd64) and macOS (M1 and above). It is designed to specifically run on a Raspberry Pi 500+ computer.

## Features

- integrated knowledge base
- support for SKILL.md (and extending Harvey by using "compiled" SKILL.md files)
- RAG support
- includes an innovative session file format based on Fountain screenplay markup. Session files  are both human readable (like reading a screenplay) and machine consumable
- friendly with other data processing tools and through shared skills with other coding agents

The name Harvey is inspired by the Mary Chase play of the same name. My little agent runs on small computers but is available to those who choose to see its value. Harvey in the play was a Púca, a mythic Celtic spirit who at times was prone to mischief. The Púca chose who could see it but even then the person doing the seeing had to be willing to see it too.

## Motivation

I think the **current AI hype cycle** will likely end with a bang and a crash. Since I started working on Harvey we've already seen the token expenses sky rocket. Many flat rate plans ration tokens. Of course if you opt to pay by tokens those services have every reason to encourage mode token consumption. That leads me to believe that things will not get cheaper. Add to that the investor's interest in aggressive cost recovery and we have a looming problem. You get a digital divide of resource availability.

Where does this leave us? The commercial platforms are just too expensive. There are problematic downsides in additional to costs (example energy consumption, model biases and data privacy issues). There needs to be an off ramp. I think an off ramp is bringing the models back in-house. The trouble is that model development has been running on an assumption about ever expanding compute resources. That's not sustainable. It doesn't match the reality today where memory pricing has gone through the ceiling and other computing components are rising too. The external GPU relied on by medium and large models have never been affordable. Time to change course. Time to revisit small models and get efficient.

Harvey is an exploration of the bring things home. This is about local control of the model system and running models on hardware that doesn't have a GPU. Harvey enables me to get useful work done with small models run via Ollama on my Raspberry Pi 500+ desktop. The Pi is a relatively low cost, low power machine. It is enough to run small models. If you have more horse power available Harvey will not mind or stand in your way.

## Where I think things are going

I see language models on a continuum like computers. In the beginning computers were large and unaffordable except by governments and the largest of corporations. They took up floors of buildings for a single machine. Eventually they became much smaller and much cheaper.  Right now Large Language models are like the huge mainframes of the 1950s and 1960s. Everyone is still thinking in terms of building sized computers. The seeds for a different approach already exist. Ollama is only one example of that. I think real innovation can happen with small models that focus on specific domains and have a direct application. We've built extremely large general purpose models in part because collective the language model community need to see how far we could take the concepts. We already seem to be plateau at huge language models. Time to take a step back and see how small models can be and be really helpful. We need a personal level model system. I think the pieces are there, they just need to be gathered together in a simpler configuration.

## Open Models, Small Models combined with generalized REPLs

The language model system space has enought maturity that we can pick the features that matter and ignore much of the initial growing pains. There is an opportunity ripe for creating tooling around the right fitness of use and purpose. The current crop of model systems (May 2026) layout base line features which are straight forward to implement. Coding agents are centered around concepts like sessions, skills, knowledge bases and retrieval augmented generation (RAG). There are plenty of papers and blog posts that descibe these features, how they work and how to use them.

The [SKILL.md](https://agentskills.io) is a good example. It was proposed by Anthropic and adopted by others (example Mistral Vibe, OpenClaw). It is also pretty easy to implement using Ollama server. The concept of [RAG](https://en.wikipedia.org/wiki/Retrieval-augmented_generation "Retrieval-augmented generation") and [MCP](https://en.wikipedia.org/wiki/Model_Context_Protocol "Model Context Protocol") seems to be picking up steam too. These to can be implemented for a resource constrained system. Mozilla AI is working on implementing a way to unify many implementation elements. An example is their [any-llm](https://www.mozilla.ai/open-tools/choice-first-stack/any-llm) project. [Ollama](https://github.com/ollama/ollama) project. It lets you build tools that integrate with many language model systems available today. Then if you build on Ollama and Hugging Face's offerings and have a foundation to roll your own tool. That's how Harvey started.

Wait, wait, why Harvey? You should use OpenClaw! OpenClaw is interesting but it is very easy to mis-configure. I wasn't confortable with that myself. I felt like OpenClaw opened my computing environment up to a whole lot of hurt. I don't want agents running around my personal communication. I don't want them messing with my editor. I want my code agent to stick to a project directory. I want it focused on what I am working on. I want a safe tool as convenient as Claude Code, GitHub CoPilot or Mistral Vibe. It should be transparent in how in works. It should let me work with the models I chose for a specific task. It should be human scale and not pit tentacles into everything else. I am working with language models on my local machine or my private network. Harvey should avoid secrets. How Harvey is configured and it's operating data should be visible to me as a human as well as any other language model system I might enlist. Harvey should let me be able to easily direct appropriate material to a remote service while keeping everything else local and processed with models in my local machine. Harvey came about because I don't see other applications that work with language models doing filling that niche.

## What is model, what is infrastructure?

Commercial SaaS language model services need to keep you engaged. They've done a good job of building up the human user interface. When you step back and look at actual implementations echo systems like those of OpenAI, Anthropic and others are not just the latest frontier model. The frontier model is important but it would not be usable without traditional web applications and services. That's telling. The model is important but the user interface and the infrastructure under it is important too. 

When a human uses a language model system they aren't asking all questions at once and in parallel. They are asking a question, or prompting for an activity. In other words there is a scope of activity. If the model chosen delivers the answers or orchestrates the actions needed does it need to be begger? As long as the interface is comfortable for the human then I think a model can be found that is fit for purpose and runs on the hardware available. If that premis it true then Harvey makes a whole lot of sense. Run the model locally (example with Ollama or a Llamafile) and pull the experience together with a simple REPL.

Useful small open models are hear [today](model_testing_plan.md). In the hype that focuses on bigger and more generalized the small specialized models seem undervalued. You can leverage the value if it is easily to run them locally with a minimum of extra features like good session management, knowledge bases and RAG. Harvey shows this can be done on small affordable hardware. Hardware that doesn't require new data centers and power plants to be built. I think smaller models, ones that can run at the edge will be the ones that carry weight in the long wrong. To explore the idea of small models on small computers I dreamed up Harvey. We'll have to wait to see where that adventure could leads.

## Release Notes

- version: 0.0.1c
- status: working proof of concept

### Authors

- Doiel, R. S.


## Software Requirements

- Go >= 1.26.2

### Software Suggestions

For building Harvey and documentation from source.

- CMTools >= 0.0.40
- Pandoc >= 3.1
- GNU Make >= 3

## Related resources

- [Getting Help, Reporting bugs](https://github.com/rsdoiel/harvey/issues)
- [LICENSE](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Installation](INSTALL.md)
- [About](about.md)
- [Documentation Index](DOCUMENTATION.md) — Complete list of all Harvey documentation
