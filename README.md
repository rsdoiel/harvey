

# harvey

![Harvey](media/harvey.svg "project mascot, a Púca")

Harvey is an agent REPL. It is written in Go and designed to use Ollama server to access language models. These can be run locally or remotely since Harvey uses Ollama's web service as the integration point. Harvey is a terminal based application. It will run on Raspberry Pi OS (Raspberry Pi 5 hardware), Linux (arm64 and amd64), Windows (arm64, amd64) and macOS (M1 and above). It is designed to specifically run on a Raspberry Pi 500+ computer.

## Features

- integrated knowledge base
- support for SKILL.md (and extending Harvey by using "compiled" SKILL.md files)
- RAG support
- innovation session files written in Fountain format making it both human readable (like reading a screenplay) and machine consumable
- friendly with other data processing tools and through shared skills with other coding agents

The name Harvey is inspired by the Mary Chase play of the same name. My little agent runs on small computers but is available to those who choose to see its value. Harvey in the play was a Púca, a mythic Celtic spirit who at times was proned to mischief. The Púca chose who could see it but even then the person doing the seeing had to be willing to see it too.

## Motivation

I think the **current AI hype cycle** will likely end with a bang and a crash. Since I started workingon Harvey we've already seen the token cost per task sky rocket. Tokens are now effectively being rationed for flat rate plans. Using commercial language model systems is unlike to get cheapre with time as the investors insist on aggresive cost recovery.

Where does this leave us? The commercial platforms are just too expensive. They have always had other problematic downsides too (example engery consumption and data privacy issues). There needs to be an off ramp. I think an off ramp is bringing the models back in-house.

Harvey is an exploration of the bring things home. This is about local control of the model system and running models on hardware that doesn't have a GPU. Harvey enables me to get useful work done with small models run via Ollama on my Raspberry Pi 500+ desktop. The Pi is a relatively low cost, low power machine. It is enough to run small models. If you have more horse power available Harvey will not mind or stand in your way.

## Where things may be going

I see language models on a contiumum like computers. In the beginning computers were large and unaffordable except by governments and the largest of corporations. They took up floors of buildings for a single machine. Eventually they became much smaller and much cheaper.  Right now Large Language models are like the huge mainframes of the 1950s and 1960s. Everyone is still thinking in terms of building sized computers. The seeds for a different approach already exist. Ollama is only one example of that. I think real innovation can happen with small models that focus on specific domains and have a direct application. We've built extremely large general purpose models in part because collective the language model community need to see how far we could take the concepts. We already seem to be plateau at huge langauge models. Time to take a step back and see how small models can be and be really helpful. We need a personal level model system. I think the pieces are there, they just need to be gathered together in a simpler configuration.

## Open Models, Small Models and a generalized REPL

There are certain things that have already become providing in the current (May 2026) crop of model systems and their tools (example code agents). [SKILL.md](https://agentskills.io) proposed by Anthropic is pretty easy to implement and is downloadable with models available today from Ollama.com.  The concept of [RAG](https://en.wikipedia.org/wiki/Retrieval-augmented_generation "Retrieval-augmented generation"). [MCP](https://en.wikipedia.org/wiki/Model_Context_Protocol "Model Context Protocol") seems to be picking up steam. The open source [Ollama](https://github.com/ollama/ollama) project has offered a way that individuals can play and learn about language models on their own since 2023. The only piece I missed from my experiments using Claude Code and cli version of CoPilot was a useful REPL and that's what got me started creating Harvey.

The ecosystem around OpenAI, Anthropic and others isn't just the latest model, it's everything the makes it easy to use. That's quite allot but not particularly revolutionary. Based decent web services and UI is what make using the model easy. That's easy to replicate locally. Harvey has started on that, first as a simple TUI REPL in the terminal but soon as a localhost service with a Web UI.

Useful open models are hear [today](model_testing_plan.md). You just need to run them easily and run them with things like a knowledge and RAG. We can do this on small affordable hardware. Hardware that doesn't require new data centers and power plants to be built. I think smaller models, ones that can run at the edge will be the ones that carry weight in the long wrong. To explore the idea of small models on small computers I dreamed up Harvey. We'll have to wait to see where that adventure could leads.

## Release Notes

- version: 0.0.1
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
