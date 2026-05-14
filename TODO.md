
# Action Items

## Bugs

- [ ] The `/ollama probe`  command does not work when referencing the model to probe with an alias
- [ ] The `/ollama use` can switch to the model when provided with an alias, ```harvey > /ollama use apertus
Now using Ollama model: apertus
harvey > What is the weather in Santa Clarita today?

Error: [ollama] model_not_found: 404 Not Found: model 'apertus' not found
harvey > /ollama alias
  Model aliases:
    apertus              → abb-decide/apertus-tools:8b-instruct-2509-q4_k_m
    gemma4               → gemma4:e2b
    quen                 → qwen2.5:7b
    qwen                 → qwen2.5:7b
```
- [ ] When using a route the response is getting written into the status message rather then being buffered and returned when complete. ```harvey > /route list

  ✗  @francis           ollama      http://localhost:8080/api/v1/chat  [(default)]
  ✓  @wren              ollama      http://wren.local:11434  [qwen2.5-coder:7b]

harvey > @wren write a Hello World program in Oberon langauge.
  → dispatching to @wren

@wren · working
  ⎿ The Owl and the Pussycat sail by the light of thought...
     ⎿ ⠴ [11s]:
  ⎿ The Jumblies have gone to sea in a sieve to fetch your answer...
     ⎿ ⠙ [13s]
     ⎿ ⠸ [14s];

     ⎿ ⠼ [15s];

     ⎿ ⠏ [17s];
  ⎿ The Dong with the luminous nose searches through the dark...
     ⎿ ⠇ [21s]!");
     ⎿ ⠋ [22s];

     ⎿ ⠙ [23s].
     ⎿ ⠏ [24s]`
  ⎿ The Nutcrackers and the Sugar-Tongs are in conference...
     ⎿ ⠏ [36s].
  ⎿ The runcible spoon stirs the pot of possibilities...
     ⎿ ⠹ [46s]:

  ⎿ Far and few, far and few, the thoughts are gathering...
     ⎿ ⠙ [49s]
     ⎿ ⠇ [50s]`

     ⎿ ⠼ [53s]:

  ⎿ The Bong-tree sways as your answer takes its shape...
     ⎿ ⠹ [55s]

  @wren
harvey > ```

## Next Steps (upcoming features, v0.0.4)

- [ ] A route should be able to be used as the current model, I should be able to use Harvey with Claude or Mistral as the working model assuming I've setup the routes and environment properly.
- [ ] By default Harvey should start up in safe mode unless the agents/harvey.yaml has been set to have it off
- [ ] The `/model alias set` should work like setting aliases in the `/ollama` commands
- [ ] Auto-complete should work with model names, otherwise I'm shelling out to see models with very long names via the ollama list command.
- [ ] An alias should not be allowed if it clashes with an existing model name or alias

