
# Unified Models Questions

Harvey is design to support three model systems, Ollama, LLamafile and Llama.cpp is in the planning stage. It will be necessary to create a unified approach to supporting the models systems. Part of the unified approach will be to capture metadata about the models appropraitely. With Ollama we can probe the models for features like tool uses, embeddings and tagged content. Ollama also provides http end points for model context sizes. Presumably there are similar features in the two other platforms. There is another set of metadata which isn't captured but will be important for applying the right model to the problem. This is information about the model's designed purposes or specailly abilties. Example is a model trained for text analsysis doesn't necessarily provide good image or code generation. Similary a model turned to generating programming code many not be good an image analysis. It is an open question if there is a standard way to describe these abailities though turms like embedding models, reasoning models, code generators are common in the literature. How do we capture this information about the models available to harvey such that the human working with Harvey can pick the right model for the specific task at hand, preferrable through the at mention syntax.



