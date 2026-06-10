# Harvey Vision

This document outlines the philosophy, motivation, and forward-looking vision behind the Harvey project.

---

## Motivation (Spring 2026)

I think the **current AI hype cycle** will likely end with a bang and a crash. Since I started working on Harvey we've already seen the token expenses sky rocket. Many flat rate plans ration tokens. Of course if you opt to pay by tokens those services have every reason to encourage more token consumption. That leads me to believe that things will not get cheaper. Add to that the investor's interest in aggressive cost recovery and we have a looming problem. There is now digital divide of resource availability based on wealth.

Where does this leave us? The commercial platforms are just too expensive. There are problematic downsides in addition to costs (example: energy consumption, model biases and data privacy issues). We need off ramps. I think one off ramp is bringing the models back in house. The trouble is that model development has been running on an assumption about ever expanding compute resources. That's not sustainable. It doesn't match the reality today where memory pricing has gone through the ceiling and other computing components are rising too. The external GPUs relied on by medium and larger models have never been affordable. Time to change course. Time to revisit small models and get efficient.

Harvey is an exploration of what is possible when we bring models home. This is about local control of the model system and running models on hardware that doesn't have a GPU. Harvey enables me to get useful work done with small models run via Ollama on my Raspberry Pi 500+ desktop. The Pi is a relatively low cost, low power machine. It is enough to run small models. If you have more horse power available Harvey will not mind or stand in your way.

---

## Where I think things are going

I see language models on a continuum like computers. In the beginning computers were large and unaffordable except by governments and the largest of corporations. They took up floors of buildings for a single machine. Eventually they became much smaller and much cheaper. Right now Large Language models are like the huge mainframes of the 1950s and 1960s. Everyone is still thinking in terms of building sized computers. The seeds for a different approach already exist. Ollama is only one example of that. I think real innovation can happen with small models that focus on specific domains and tuned to specific application needs. We've built extremely large general purpose models in part because of a collective desire to see how far we could take these concepts. I think there is a plateau in creating ever larger models. Time to take a step back and see how small models can be while being really helpful. To move things forward I believe we will need a personal sized model. I think this could open interesting doors much as personal computing opened the door to what we have today. I think the pieces are there. They just need to be assembled in a simpler configuration. (2026-05, RSD)

---

## Open Models, Small Models combined with generalized REPLs

The language model system space has enough maturity that we can pick the features that matter and ignore much of the initial growing pains. There is an opportunity ripe for creating tooling around fitness of use and purpose. The current crop of model systems (May 2026) layout baseline features which are straight forward to implement. Coding agents are centered around concepts like sessions, skills, knowledge bases and retrieval augmented generation (RAG). There are plenty of papers and blog posts that describe these features, how they work and how to use them and importantly how to build them.

The [SKILL.md](https://agentskills.io) is a good example. It was proposed by Anthropic and adopted by others (example Mistral Vibe, OpenClaw). It is also pretty easy to implement using Ollama server. The concept of [RAG](https://en.wikipedia.org/wiki/Retrieval-augmented_generation "retrieval-augmented generation") was easy to implement. Some trends like [MCP](https://en.wikipedia.org/wiki/Model_Context_Protocol "Model Context Protocol") strike me as a way to run up token usage. I remain on the fence about its usefulness. Pick the proven trends that favor limited resource availability and implement them is the approach I'm taking with Harvey. Harvey isn't being developed in a vacuum. Mozilla AI is working on implementing a way to unify many implementation elements. An example is their [any-llm](https://www.mozilla.ai/open-tools/choice-first-stack/any-llm) project, [Ollama](https://github.com/ollama/ollama) project. Harvey can take advantage of that too. This approach lets me evolve Harvey as a small project while allowing me to explore the domain of small model computing on small computers. That's how Harvey started.

### Wait, wait, why Harvey? You should use OpenClaw!

OpenClaw is interesting but it is very easy to mis-configure. I wasn't comfortable with that myself. I felt like OpenClaw opened my computing environment up to a whole lot of hurt. I don't want agents running around my personal communication. I don't want them messing with my editor. I want my code agent to stick to a project directory. I want it focused on what I am working on. I want a safe tool as convenient as Claude Code, GitHub CoPilot or Mistral Vibe. It should be transparent in how it works. It should document what it does. It should let me work with the models I chose for a specific task. It should be human scale and not put tentacles into everything else. I am working with language models on my local machine and my private network. Harvey should let me be able to easily direct appropriate material to a remote service while keeping everything else local so I can continue the processing with models in my local workspace. Harvey came about because I didn't see other applications approaching model systems this way. Safety and locality first.

---

## What is a model? What is infrastructure?

Commercial SaaS language model services need to keep you engaged. They've done a good job of building up the human user interface. The conversations use techniques that seem similar to the [BITE](https://freedomofmind.com/cult-mind-control/bite-model-pdf-download/) cult recruitment methodology described by Steven Hassan over the last many decades. When you step back and look at actual implementations the ecosystems like those of OpenAI, Anthropic and others are not just the latest frontier model they include considerable effort in implementing the human user interface to keep engagement up. The frontier models are important but they're not mysterious when you look at how everything is wrapped up in traditional web services. They've baked in the engagement business model. This is true whether they used a language model to generate the code or not. It's telling. The model is important but the user interface and the infrastructure under it is important too. The engagement wrappings make the contact with us humans. It's important to keep that in mind.

On a small scale we can build our own language model system. Like the SaaS services we can take advantage of traditional software programs and methods too. That is the core of Harvey. It's just a REPL that is designed to use modeling services through web protocols. Unlike the SaaS offerings it is under our control running on our hardware. It takes advantage of our limited resources and single user orientation. Harvey does less so what would be done in parallel by a SaaS service is done sequentially in Harvey. **There is an opportunity on a locally run language model system to scale down instead of up.**

---

## What's next

I believe useful small open models are [here today](model_guide.md). The trick will be is identifying those models that are mature enough to use day in and day out safely. In the hype that focuses on bigger and more generalized the small specialized models seem undervalued. We can leverage the value if it is [easily to run them locally](Llamafile_notes.md "LLamafile is a form Mozilla AI is experimenting with that looks promising"). This is true especially if there are some key additional features like good session management, knowledge bases and RAG. Harvey shows we can have some of those bells and whistles while running on small affordable hardware. Hardware that doesn't require new data centers and power plants to be built. I think smaller models, ones that can run at the edge will be the ones that carry weight in the long run. To explore the idea of small models on small computers I dreamed up Harvey. We'll have to wait to see where that adventure could lead.
