

# Harvey

![Harvey, a six foot six invisible rabbit](media/harvey.svg "project mascot, a Púca")

Harvey is an agent REPL. It is written in Go and designed to use Ollama server to access language models. These can be run locally or remotely since Harvey uses Ollama's web service as the integration point. Harvey is a terminal based application. It will run on Raspberry Pi OS (Raspberry Pi 5 hardware), Linux (arm64 and amd64), Windows (arm64, amd64) and macOS (M1 and above). It is designed to specifically run on a Raspberry Pi 500+ computer.

## Features

- integrated knowledge base
- support for SKILL.md (and extending Harvey by using "compiled" SKILL.md files)
- knowledge base with [RAG](https://en.wikipedia.org/wiki/Retrieval-augmented_generation "retrieval-augmented generation") support
- includes an innovative session file format based on Fountain screenplay markup. Session files  are both human readable (like reading a screenplay) and machine consumable
- friendly with other data processing tools and through shared skills with other coding agents

The name Harvey is inspired by the Mary Chase play of the same name. My little agent runs on small computers but is available to those who choose to see its value. Harvey in the play was a Púca, a mythic Celtic spirit who at times was prone to mischief. The Púca chose who could see it but even then the person doing the seeing had to be willing to see it too.

## Motivation (Spring 2026)

I think the **current AI hype cycle** will likely end with a bang and a crash. Since I started working on Harvey we've already seen the token expenses sky rocket. Many flat rate plans ration tokens. Of course if you opt to pay by tokens those services have every reason to encourage mode token consumption. That leads me to believe that things will not get cheaper. Add to that the investor's interest in aggressive cost recovery and we have a looming problem. You get a digital divide of resource availability.

Where does this leave us? The commercial platforms are just too expensive. There are problematic downsides in additional to costs (example energy consumption, model biases and data privacy issues). There needs to be an off ramp. I think an off ramp is bringing the models back in house. The trouble is that model development has been running on an assumption about ever expanding compute resources. That's not sustainable. It doesn't match the reality today where memory pricing has gone through the ceiling and other computing components are rising too. The external GPU relied on by medium and large models have never been affordable. Time to change course. Time to revisit small models and get efficient.

Harvey is an exploration of the bring things home. This is about local control of the model system and running models on hardware that doesn't have a GPU. Harvey enables me to get useful work done with small models run via Ollama on my Raspberry Pi 500+ desktop. The Pi is a relatively low cost, low power machine. It is enough to run small models. If you have more horse power available Harvey will not mind or stand in your way.

## Where I think things are going

I see language models on a continuum like computers. In the beginning computers were large and unaffordable except by governments and the largest of corporations. They took up floors of buildings for a single machine. Eventually they became much smaller and much cheaper.  Right now Large Language models are like the huge mainframes of the 1950s and 1960s. Everyone is still thinking in terms of building sized computers. The seeds for a different approach already exist. Ollama is only one example of that. I think real innovation can happen with small models that focus on specific domains and have a direct application. We've built extremely large general purpose models in part because of a collective desire to see how far we could take these concepts. I think their is a plateau in creating ever larger models. Time to take a step back and see how small models can be while being really helpful. To move things forward I believe we will need a personal sized model. I think this could open interesting doors much as personal computing opened the door to what we have today. I think the pieces are there. They just need to be assembled in a simpler configuration. (2026-05, RSD)

## Open Models, Small Models combined with generalized REPLs

The language model system space has enough maturity that we can pick the features that matter and ignore much of the initial growing pains. There is an opportunity ripe for creating tooling around fitness of use and purpose. The current crop of model systems (May 2026) layout base line features which are straight forward to implement. Coding agents are centered around concepts like sessions, skills, knowledge bases and retrieval augmented generation (RAG). There are plenty of papers and blog posts that describe these features, how they work and how to use them and importantly how to build them.

The [SKILL.md](https://agentskills.io) is a good example. It was proposed by Anthropic and adopted by others (example Mistral Vibe, OpenClaw). It is also pretty easy to implement using Ollama server. The concept of [RAG](https://en.wikipedia.org/wiki/Retrieval-augmented_generation "Retrieval-augmented generation") was easy to implement. Some trends like [MCP](https://en.wikipedia.org/wiki/Model_Context_Protocol "Model Context Protocol") strike me as a way to run up token usage. I remain on the fense about it's usefulness. Pick the proven trends that favor limitted resource availability and implement them is the approach I'm taking with Harvey. Harvey isn't being developed in a vacuum. Mozilla AI is working on implementing a way to unify many implementation elements. An example is their [any-llm](https://www.mozilla.ai/open-tools/choice-first-stack/any-llm) project. [Ollama](https://github.com/ollama/ollama) project. Harvey can take advantage of that too. This approach let's me evolve Harvey as a small project while allowing me to explore the domain of small model computing on small computers. That's how Harvey started.

### Wait, wait, why Harvey? You should use OpenClaw!

OpenClaw is interesting but it is very easy to mis-configure. I wasn't comfortable with that myself. I felt like OpenClaw opened my computing environment up to a whole lot of hurt. I don't want agents running around my personal communication. I don't want them messing with my editor. I want my code agent to stick to a project directory. I want it focused on what I am working on. I want a safe tool as convenient as Claude Code, GitHub CoPilot or Mistral Vibe. It should be transparent in how in works. It should document what it does. It should let me work with the models I chose for a specific task. It should be human scale and not put tentacles into everything else. I am working with language models on my local machine or my private network. Harvey should let me be able to easily direct appropriate material to a remote service while keeping everything else local so I can continue the processing with models in my local machine. Harvey came about because I don't see other applications that work with language models doing filling that niche.

NOTE: I have made an attempt to keep Harvey safe as I've designed Harvey. Harvey avoids secrets. How Harvey is configured and it's operating data is visible to me as a human. There is risk in using language model agents because you are allowing a probablistic text model to direct the execution of commands. That's just going to an attack surface for mischief. I've have placed limitations on some of Harvey's capabilities as a result. It's not perfect by a long short and hopefully I will be able to lower the risks over time. This is one of the areas I think will be critical in evolving person language model tooling. As a result you should keep in mind that Harvey is experimental, it is only a working proof of concept. Don't use Harvey where the risks might endanger your data or person.

## What is a model? What is infrastructure?

Commercial SaaS language model services need to keep you engaged. They've done a good job of building up the human user interface. The conversations use technique that seem similar to the [BITE](https://freedomofmind.com/cult-mind-control/bite-model-pdf-download/) recruitment methodology described by Steven Hassan. When you step back and look at actual implementations the ecosystems like those of OpenAI, Anthropic and others are not just the latest frontier model they include considerable effort in implementing the human user interface to keep engagement up. The frontier models are important but their not mysterious when you look at how everything wrapped up traditional web services. This is true weather they used a language model to generate the code or not. That's telling. The model is important but the user interface and the infrastructure under it is important too. That's what has contact with us humans.

When building our own language model system most of it can take advantage of traditional software programs and methods too.  I suspect you can do allot before sending the resulting text as a prompt to the model for processing. What is done in parallel for the sake of scaling can be done sequentially if we're at the scale of a single user. **There is an opportunity on a locally run language model system to scale down instead of up.**

I believe useful small open models are hear [today](model_guide.md). The trick is identifying those that are mature enough to use day in and day out safely. In the hype that focuses on bigger and more generalized the small specialized models seem undervalued. You can leverage the value if it is [easily to run them locally](llamafile_notes.md "LLamafile is a form Mozilla AI is experimenting with that looks promising"). This is true especially if there are some key additional features like good session management, knowledge bases and RAG. Harvey shows this can be done on small affordable hardware. Hardware that doesn't require new data centers and power plants to be built. I think smaller models, ones that can run at the edge will be the ones that carry weight in the long wrong. To explore the idea of small models on small computers I dreamed up Harvey. We'll have to wait to see where that adventure could leads.

## Release Notes

- version: 0.0.3
- status: working proof of concept

### Authors

- Doiel, R. S.


## Software Requirements

- Go >= 1.26.2

### Software Suggestions

For building Harvey and documentation from source.

- CMTools >= 0.0.43
- Pandoc >= 3.9
- GNU Make >= 3.8

## Related resources

- [Getting Help, Reporting bugs](https://github.com/rsdoiel/harvey/issues)
- [LICENSE](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Installation](INSTALL.md)
- [About](about.md)
- [Documentation Index](DOCUMENTATION.md) — Complete list of all Harvey documentation
