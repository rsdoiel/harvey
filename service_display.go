// Package harvey — service_display.go decides how a model and the service it
// runs on are shown (DR-0049 item 6): the bare model when only one service is
// in play, service/model when there is a choice.
package harvey

/** serviceCount returns how many model services this session can use: the
 * Ollama server when it answers, and the Hailo server when one is detected.
 * It is worked out once per session, since it probes the servers.
 *
 * Returns:
 *   int — 0, 1 or 2 with the two default services (DR-0049 item 10).
 *
 * Example:
 *   if a.serviceCount() > 1 { fmt.Println("more than one service") }
 */
func (a *Agent) serviceCount() int {
	if a.serviceCountCache > 0 {
		return a.serviceCountCache - 1
	}
	n := 0
	f := a.ollamaFamily()
	if f.OllamaURL != "" && ProbeOllama(f.OllamaURL) {
		n++
	}
	if f.HailoURL != "" {
		n++
	}
	a.serviceCountCache = n + 1
	return n
}

/** showService reports whether the service is shown beside a model name: true
 * when more than one service is in play, so a CPU-only machine never prints
 * the word "ollama".
 *
 * Returns:
 *   bool — true with two or more services.
 *
 * Example:
 *   if a.showService() { label = service + "/" + model }
 */
func (a *Agent) showService() bool { return a.serviceCount() > 1 }

/** displayModel is how a model is shown to a person: "service/model" when
 * there is a choice of services (the form /model use and -m accept), the bare
 * model name otherwise.
 *
 * Parameters:
 *   service (string) — the service name, "ollama" or "hailo".
 *   model   (string) — the model name.
 *
 * Returns:
 *   string — what to print.
 *
 * Example:
 *   a.displayModel("hailo", "llama3.2:3b") // "hailo/llama3.2:3b" with two services
 */
func (a *Agent) displayModel(service, model string) string {
	if service == "" || !a.showService() {
		return model
	}
	return service + "/" + model
}

/** clientDisplay is displayModel for the model a client talks to; a client that
 * is not an Ollama-shaped one is shown by its own Name().
 *
 * Parameters:
 *   c (LLMClient) — the client, nil for none.
 *
 * Returns:
 *   string — "none" for nil, else what to print.
 *
 * Example:
 *   fmt.Println(a.clientDisplay(a.Client)) // "hailo/llama3.2:3b" or "llama3.2:3b"
 */
func (a *Agent) clientDisplay(c LLMClient) string {
	if c == nil {
		return "none"
	}
	if ac, ok := c.(*OllamaChatClient); ok {
		svc := ac.Engine()
		if svc == "" {
			svc = ac.ProviderName()
		}
		return a.displayModel(svc, ac.ModelName())
	}
	return c.Name()
}

/** drafterDisplay is how the drafting model is named on screen. A drafter
 * built from the session's own client is shown as clientDisplay shows it; one
 * named by the person (@NAME) keeps the name they gave. The generated_by
 * record still uses Drafter.Name().
 *
 * Parameters:
 *   d (Drafter) — the drafter chosen for /kb draft.
 *
 * Returns:
 *   string — what to print after "Drafting with".
 *
 * Example:
 *   fmt.Fprintf(out, "Drafting with %s\n", a.drafterDisplay(d))
 */
func (a *Agent) drafterDisplay(d Drafter) string {
	if ld, ok := d.(*llmDrafter); ok && ld.name == "" {
		return a.clientDisplay(ld.client)
	}
	return d.Name()
}
