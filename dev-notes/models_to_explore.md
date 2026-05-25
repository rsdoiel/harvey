
# Models to try and evaluate

Working from least resource demanding to most

| Ollama Model Name | Description                                                                 | tools | embedding | tags |
|-------------------|-----------------------------------------------------------------------------|-------|-----------|------|
| qwen3:8b          | Use these for auto-complete and simple refactoring, not complex generation. |       |           |      |
| deepseek-r1:14b   | Best reasoning. Thinks through complex bugs.                                |       |           |      |
| qwen3:14b         | Good coding + fast inference.                                               |       |           |      |
| granite3-dense:2b, granite3-dense:8b | Dense model based on Granite 3 series                    |       |           |      |
| ibm/granite3.3-guardian:8b | I think this model might evaluate security risks                   |       |           |      |
| ibm/granite4.1:3b, ibm/granite4.1:8b | This model is designed to handle general instruction-following tasks and can be integrated into AI assistants across various domains, including business applications. |       |           |      |



## IBM/Granite 4

Granite 4.1 is a family of dense language models available in three sizes: 3B, 8B, and 30B parameters. Each size is available in both base and instruction-tuned variants, with optional FP8 quantization for efficient deployment. Built with a dense architecture, Granite 4.1 demonstrates significant improvements over Granite 4.0 in tool calling, instruction following, coding capabilities, and mathematical reasoning.
​
Model Variants

    granite-4.1-3b-base & granite-4.1-3b-instruct: Compact model optimized for edge deployment and resource-constrained environments
    granite-4.1-8b-base & granite-4.1-8b-instruct: Balanced model for general-purpose enterprise applications
    granite-4.1-30b-base & granite-4.1-30b-instruct: High-capacity model for complex reasoning and specialized tasks

All models are released under the Apache 2.0 license with cryptographic signatures, ISO certification, and full transparency disclosures.

- [Granite 4.1, website](https://www.ibm.com/granite/docs/models/granite4-1)
- [AI Risk Atlas](https://www.ibm.com/docs/en/watsonx/saas?topic=ai-risk-atlas)

