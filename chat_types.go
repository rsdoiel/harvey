package harvey

/** FunctionCall is the function a model asked to call.
 *
 * Fields:
 *   Name      (string) — the tool's name.
 *   Arguments (string) — the arguments as a JSON object, encoded as a string.
 *
 * Example:
 *   fc := FunctionCall{Name: "read_file", Arguments: `{"path":"main.go"}`}
 */
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

/** ToolCall is a tool call made by the model.
 *
 * Fields:
 *   Function (FunctionCall) — which tool, with what arguments.
 *   ID       (string)       — identifies the call; Ollama does not always supply one, so the client makes up call_<n>.
 *   Type     (string)       — always "function".
 *
 * Example:
 *   tc := ToolCall{ID: "call_0", Type: "function", Function: FunctionCall{Name: "read_file", Arguments: "{}"}}
 */
type ToolCall struct {
	Function FunctionCall `json:"function"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
}

/** Function describes a tool the model may call.
 *
 * Fields:
 *   Name        (string)         — the tool's name.
 *   Description (string)         — what the tool does, shown to the model.
 *   Parameters  (map[string]any) — a JSON Schema object for the arguments.
 *
 * Example:
 *   f := Function{Name: "read_file", Parameters: map[string]any{"type": "object"}}
 */
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

/** Tool is a tool offered to the model.
 *
 * Fields:
 *   Type     (string)   — always "function".
 *   Function (Function) — the tool's definition.
 *
 * Example:
 *   t := Tool{Type: "function", Function: Function{Name: "read_file"}}
 */
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

/** ImageURL is an image carried in a message as a data URL.
 *
 * Fields:
 *   URL    (string) — "data:image/png;base64,..." ; only data URLs are sent.
 *   Detail (string) — unused by Ollama; kept so recorded messages round-trip.
 *
 * Example:
 *   img := &ImageURL{URL: "data:image/png;base64,iVBOR..."}
 */
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

/** ContentPart is one part of a multimodal message.
 *
 * Fields:
 *   Type     (string)    — "text" or "image_url".
 *   Text     (string)    — the text, for a text part.
 *   ImageURL (*ImageURL) — the image, for an image_url part.
 *
 * Example:
 *   parts := []ContentPart{{Type: "text", Text: "what is this?"}}
 */
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

/** Capabilities lists what a model server can do. The values are static for
 * the Ollama client; no request is made to learn them.
 *
 * Example:
 *   if c.ProviderCapabilities().CompletionImage { attachImage() }
 */
type Capabilities struct {
	Completion          bool
	CompletionImage     bool
	CompletionPDF       bool
	CompletionReasoning bool
	CompletionStreaming bool
	CompletionTools     bool
	Embedding           bool
	ListModels          bool
}
