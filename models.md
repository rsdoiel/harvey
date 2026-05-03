
# Models

This document contains the current list of models if you want to start playing with Harvey and Ollama on a system with limited resources (example a Raspberry Pi 5 with >= 8Gig of RAM).

## Settting up

Taking the models for a spin. 

~~~shell
# Startup Harvey in your work directory
harvey
# Pull some models to use with Harve
/ollama pull phi4-mini:3.8b
/ollama pull ibm/granite4.1:3b
/ollama pull granite-code:3b
/ollama pull granite3-moe:3b
/ollama pull qwen2.5-coder:latest
# Pull some models to support Retrieval Augmented Generation (RAG)
/ollama pull nomic-embed-text-v2-moe
/ollama pull nomic-embed-text
/ollama pull mxbai-embed-large
/ollama pull bge-small-en-v1.5
/ollama pull bge-m3
# List your models
/ollama list
# Use a model
/ollama use ibm/granite4.1:3b
# Now you're ready to play
# Exit harvey using the /exit command
/exit
~~~

## Building a RAG

Here's the basic steps.

1. Create a directory where you'll put the Markdown traning files
2. Collect the Markdown documents you want to use to build the knowledge and make available to the model
3. Start up harvey and check that things are setup
4.

Steps 1 and 2 are left to the user. If you doing work with Deno and TypeScreipt you might grab the documentation GitHub repository for Deno to start with. If you were doing front end work you might do the same for the Mozilla Development Network docs. It's up to you.

The ingest process to build the knowledge base and embeddings works well using Markdown documents. So I suggest starting with small collection. Here's an example I did for working with Deno+TypeScript.

~~~shell
mkdir TrainingMaterials
cd TrainingMarterials
git clone git@github.com:denoland/docs.git DenoDocs
cd ..
~~~

This what I did for steps 3 and 4.

~~~shell
harvey
# Let's use the ibm/granite4.1:3b model
/ollama use ibm/granite4.1:3b
# Now let's setup our RAG
/rag setup
# Now ingest our content (this will take a while)
/rag ingest TrainingMaterials/DenoDocs
# Now you're ready to play, here's an example prompt
Show me the source code for a Hello World program for Deno+TypeScript. The program should say "Hi there!", then prompt for the username's name, then say "Hi <username>, glad to meet you!".
# After a wait if all worked well you should see a TypeScript example
/exit
~~~

