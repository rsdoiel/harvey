
# Model evaluation

## Single model evalations

Current models being evaluated. These are the current canidates for use with Harvey running on a local machine.
  
~~~models
phi4-mini:3.8b
ibm/granite4.1:3b
granite-code:3b
smollm2:1.7b
abb-decide/apertus-tools:8b-instruct-2509-q4_k_m
pstdio/microcoder:3b
mistral-nemo:latest
llama3.2:latest
qwen2.5-coder:7b
mistral:latest
qwen2.5-coder:latest
ministral-3:3b
ministral-3:14b
granite3-moe:1b
granite3-moe:3b
granite3.3:2b
MeetSolanki/MeetAISmall:latest
aisingapore/Apertus-SEA-LION-v4-8B-IT:latest
gemma4:latest
~~~

Embedding models for Retrieval Augementation Generation (RAG)

~~~models
nomic-embed-text-v2-moe:latest
nomic-embed-text:latest
mxbai-embed-large:latest
bge-small-en-v1.5:3b
bge-m3:567m
~~~

### granite-code:3b

This model doesn't support tools but could be used by a model that did support tools or I could figure out how to use it to derive tool support for it. It was fast the code reasonable.

~~~harvey
/clear
/ollama use granite-code:3b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results

~~~
Certainly! Here's an example implementation of the program in Go:
```go
package main

import (
	"fmt"
	"os"
)

func main() {
	name, err := getName()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting name:", err)
		return
	}

	fmt.Println("Hi there, my name is", name)
}

func getName() (string, error) {
	var name string
	fmt.Print("What's your name? ")
	_, err := fmt.Scanln(&name)
	if err != nil {
		return "", err
	}

	return name, nil
}
```
This program uses the `getName` function to get the user's name from the terminal. The `main` function then displays a greeting message with the user's name and waits for the user to press Enter before exiting.

 Ollama (granite-code:3b) · 212 reply + 4011 ctx · 36.065s · 14.0 tok/s
~~~

### stable-code

Seems quick in Go, got lots on a similar prompt using Deno TypeScript. Might be useful to augment with a RAG in Harvey at some point.

~~~harvey
/clear
/ollama use stable-code
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

response

~~~harvey

Here's a simple Go program that implements the described functionality:

```go
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	message := "Hi there, what's your name?"
	reader := bufio.NewReader(os.Stdin))
	fmt.Printf("%s\n", message))
	name, err := reader.ReadString('\n')).TrimSpace()
	if err != nil {
		fmt.Println("Error: Failed to read name.", err)
	} else {
		repliedMessage := fmt.Sprintf("Nice to meet you, %s! Let's continue exploring Go language.", name))
		fmt.Printf("%s\n", repliedMessage))
	}
}
```

You can run this program using the `go run` command or by saving it in a file and running `go build` followed by `./your_binary_name`.
  Ollama (stable-code) · 221 reply + 1650 ctx · 1m31.809s · 2.6 tok/s
harvey >
~~~

### codellama:13b

Reasonable response on macmini-rd.local. I have concerns about this given the very public messy history of Meta's LLM development.

~~~harvey
/clear
/ollama use codellama:13b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Result

~~~harvey
harvey > Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.

Sure! Below is a simple Go program that does what you asked for.

```go
cmd/hello/main.go  ─ space-separated lang path

package main

import (
	"fmt"
)

func main() {
	fmt.Println("Hi there, what's your name?")
	var name string
	fmt.Scanln(&name)
	fmt.Printf("Hello, %s!\n", name)
}
```

This program will print a greeting, prompt the user for their name, and then respond with a personalized message.
  Ollama (qwen2.5-coder:7b) · 103 reply + 991 ctx · 8.567s · 12.6 tok/s
harvey > /clear
Conversation history cleared. New session #5 started.
harvey > /ollama use codellama:13b
Now using Ollama model: codellama:13b
harvey > Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.

```go
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Hi there, what's your name?")
	name, _ := reader.ReadString('\n')
	fmt.Printf("Hello, %s!\n", name)
}
```
  Ollama (codellama:13b) · 99 reply + 1183 ctx · 38.36s · 6.7 tok/s
~~~


### qwen2.5-coder:7b

Reasonable quick on macmini-rd.local. Looks correct and wrote a file.

~~~harvey
/clear
/ollama use qwen2.5-coder:7b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results

~~~harvey
harvey > Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.

```go:cmd/hello/main.go
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	fmt.Println("Hi there, what's your name?")
	reader := bufio.NewReader(os.Stdin)
	name, _ := reader.ReadString('\n')
	fmt.Printf("Hello, %s", name)
}
```
  Ollama (qwen2.5-coder:7b) · 70 reply + 991 ctx · 17.855s · 12.3 tok/s
  ┌─ Write: cmd/hello/main.go ────────────────────────┐
  │  package main
  │
  │  import (
  │  	"bufio"
  │  	"fmt"
  │  	"os"
  │  )
  │
  │  … (6 more lines)
  └───────────────────────────────────────────────────────┘
  [y]es  [n]o  [A]ll  [q]uit > a
  ✓ wrote cmd/hello/main.go (212 bytes)
~~~


### deepseek-coder-v2

Worked reasonably quickly and returned a valid Go program.

~~~harvey
/clear
/ollama use deepseek-coder-v2
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Response

~~~harvey
harvey > Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.

 ```go:cmd/hello/main.go
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Hi there, what's your name?")
	name, _ := reader.ReadString('\n')
	fmt.Printf("Hello, %s! Nice to meet you.\n", name)
}
```
  Ollama (deepseek-coder-v2) · 104 reply + 1104 ctx · 15.48s · 28.0 tok/s
~~~

### codestral:latest

Initial impression it is too slow. Killed after nine minutes, removed.

~~~harvey
/clear
/ollama use codestal:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

### MeetSolanki/MeetAISmall:latest

~~~harvey
/clear
/ollama use MeetSolanki/MeetAISmall:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	```go
	package main

	import "fmt"

	func main() {
	    fmt.Println("Hi there, what's your name?")
	    name := ""
	    fmt.Print("Enter your name: ")
	    fmt.Scanln(&name)
	    fmt.Println("Hello, " + name + "!") // Output after user input
	}
	```
	  1003 prompt + 481 reply tokens · 1m18.315s · 6.3 tok/s

~~~

### sailor2:1b


~~~harvey
/clear
/ollama use sailor2:1b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	**Go Program:**

	```go
	package main

	import (
		"fmt"
	)

	// FountainAnalysis is our existing fountain analysis skill
	func fountain-analysis() {
		defer fmt.Println("Hello, World!")
	}

	type Name struct {
		Name string `json:"name"`
	}

	func (n *Name) GetName() string {
		if n.Name == nil {
			return "Hello World!"
		}
		return n.Name
	}

	func main() {
		// Define the name to be displayed with fountain-analysis skill
		name := Name{"Hi there! What's your name?"}

		// Call the FountainAnalysis function and display the result
		fountain-analysis()
		fmt.Println("Your name:", name)
	}
	```

	**Explanation:**

	1. **Define `fountain-analysis`**: This struct represents a named stream (Name) that can be triggered by a provided path (`/testout/hello.bash`) when executed.
	2. **Create the FountainAnalysis Function**: 
	   - `fountain-analysis()` is called with a template `<name>` to inject an example message into context.
	3. **Define `GetName` Method**: The name of the user's name is retrieved from the JSON representation (in this case, `"Hi there! What's your name?"`).
	4. **Main Function**:
	   - Define and pass the name as a template to `fountain-analysis()`.
	   - Print the result using the `fmt.Println()` function.

	**Note:** Adjust paths and templates for actual file locations if different.

	This script will successfully display "Hi there, what's your name?" message to the user when triggered by the provided code block. Enjoy!
	  951 prompt + 335 reply tokens · 1m12.406s · 8.6 tok/s

~~~

### stable-code:3b


~~~harvey
/clear
/ollama use stable-code:3b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	Here's a simple Go program that implements the described behavior:

	```go
	package main

	import (
		"bufio"
		"fmt"
		"os"
	)

	func main() {
		message := "Hi there, what's your name?"

		reader := bufio.NewReader(os.Stdin))

		fmt.Println(message)

		name, _ := reader.ReadString('\n')).TrimSpace()

		reply := fmt.Sprintf("Hello %s! Nice to meet you.", name))

		fmt.Println(reply))
	}
	```

	This program uses the `bufio` package to read input from the user. It first displays a message asking for the user's name. Then, it reads the user's input as a string and trims any leading or trailing whitespace. Finally, it constructs a reply using the user's name and then prints both the original message and the reply to the console.
	  1127 prompt + 211 reply tokens · 3m35.384s · 3.3 tok/s
~~~

NOTE: Need to check if this is a good canidate for a model to route for more complex coding.

### smollm:135m


~~~harvey
/clear
/ollama use smollm:135m
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

Here is an example of a Go program that displays "Hi there, what's your name?" prompt:
```go/program
--name
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
```
To implement this program, you can follow these steps:

1. Define a Go function that takes in the name and prompts for it using `go/prompt`.
2. Call the function with the prompt to display "Hi there, what's your name?"
3. The function will then call another function that displays "Hello" or "Goodbye".
4. The function will also print out a message indicating that the user has not provided their name before.
5. The program will terminate when it reaches this point and return `None`.
6. Call the function with the prompt to display "Hi there, what's your name?" again to display "Hello" or "Goodbye".
7. Call the function with the prompt to display "What's your name?". This is done by calling the function with the prompt that starts with `hi`, followed by a space and then the user's name.
8. The program will terminate when it reaches this point and return `None`.
9. Call the function again with the prompt to display "Goodbye" or "What's your name?". This is done by calling the function with the prompt that starts with `hi`, followed by a space, then the user's name.
10. The program will terminate when it reaches this point and return `None`.
  1046 prompt + 364 reply tokens · 19.533s · 35.4 tok/s
  ┌─ Write: go/program ───────────────────────────────┐
  │  --name
  │  hi
  │  hi
  │  hi
  │  hi
  │  hi
  │  hi
  │  hi
  │  … (22 more lines)
  └───────────────────────────────────────────────────────┘
  [y]es  [n]o  [A]ll  [q]uit > y
  ✓ wrote go/program (94 bytes)
~~~

NOTE: This is a hulicination, but it did write a file in go/program that had all those lines of "hi". I removed this model form my Ollama setup.

### smollm:360m

~~~harvey
/clear
/ollama use smollm:360m
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	Here is a simple Go program that demonstrates how to display a greeting message:
	```go
	package main

	import (
	    "fmt"
	    "net/http"
	    "string"
	)

	func main() {
	    // Create a new HTTP client instance
	    httpClient := http.Client(http.Handle("http://localhost").Handle("/hello"))

	    // Send the greeting message to the client
	    fmt.Println("Hi there, what's your name?")
	    response = httpClient.SendAsync(fmt.Sprintf("%s", "Hello, %@!\n"), string.Empty)
	    fmt.Println(response)
	}
	```
	Here's how it works:

	1. We create a new HTTP client instance using `http.Client` and set the handle to "/hello" for the client that will receive the greeting message.
	2. We send the greeting message using `fmt.Sprintf()` with the format "%s", which is a string literal that concatenates the name and "Hello, %@!\n".
	3. The client receives the message and sends it back to us using `httpClient.SendAsync()`.
	4. We use `fmt.Println` to print the response from the client.
	5. Finally, we return from the program.

	To test this code, you can run it like so:
	```go
	# Start a new HTTP client instance
	httpClient := http.Client(http.Handle("/hello"))

	# Send the greeting message to the client
	response = httpClient.SendAsync("Hi there, what's your name?")

	# Print the response from the client
	println(response)
	```
	This program will display a "Hello, my name is [name]!" message when it receives a request for an HTTP greeting.
	  1046 prompt + 392 reply tokens · 40.442s · 20.1 tok/s

~~~

NOTE: This did return a Go program, oddly a web service. Keeping it around for further testing. After testing on another machine, discarding it for now.

### smollm:1.7b

~~~harvey
/clear
/ollama use smollm:1.7b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	Here is a Go program that demonstrates this:
	```go
	package main

	import (
		"fmt"
		"fmt"
		"net/http"
	)

	func main() {
		name := fmt.Sprintf("Hello, what's your name?", "John Doe")
		http.HandleFunc("/", (w http.ResponseWriter, r *http.Request) => {
			http.Println(w.Header(), w.Body())
		})

		http.ListenAndServe(":80", nil)
	}
	```
	  1046 prompt + 128 reply tokens · 2m4.536s · 5.4 tok/s
~~~

NOTE: Response was a Go program, like previous model assumes a web server for some reason. Keep around for further testing.  for now, when testing on another machine I get better results but not correct. Discarding for now.

### granite3.3:2b

~~~harvey
/clear
/ollama use granite3.3:2b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	```bash
	go:cmd/hello/main.go
	```

	```go
	package main

	import (
	    "bufio"
	    "fmt"
	    "os"
	    "strings"
	)

	func main() {
	    reader := bufio.NewReader(os.Stdin)

	    fmt.Println("Hi there, what's your name?")
	    var name string
	    fmt.Scanln(&name) // Reads a line of input and stores it as a string in 'name'.

	    greeting := fmt.Sprintf("Hello, %s!", name)
	    fmt.Print(greeting)
	}
	```
	  1036 prompt + 131 reply tokens · 2m35.077s · 3.5 tok/s

~~~

NOTE: This was relatively quick and a correct response. Worked well on my Windows ARM box.

### ibm/granite4:3b

~~~harvey
/clear
/ollama use ibm/granite4:3b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

```go:src/main.go
package main

import (
	"bufio"
	"fmt"
)

func main() {
	reader := bufio.NewReader(input)
	fmt.Print("Hi there, what's your name? ")

	name, _ := reader.ReadString('\n')
	fmt.Printf("\r\nHello %s!\r\n", name)
}
```
  951 prompt + 68 reply tokens · 2m44.781s · 2.8 tok/s
  ┌─ Write: src/main.go ──────────────────────────────┐
  │  package main
  │  
  │  import (
  │  	"bufio"
  │  	"fmt"
  │  )
  │  
  │  func main() {
  │  … (6 more lines)
  └───────────────────────────────────────────────────────┘
  [y]es  [n]o  [A]ll  [q]uit > y
  ✓ wrote src/main.go (211 bytes)

~~~

NOTE: The model offered to write out the program to the file system with an appropriate prompt. It correctly created the file directory and file. This is a keeper.

### ibm/granite4:350m

~~~harvey
/clear
/ollama use ibm/granite4:350m
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	```go
	package main

	import (
		"fmt"
	)

	func greet(name string) string {
		fmt.Println("Hi there, what's your name?")
		return fmt.Sprintf("%s", name)
	}

	func main() {
		name := "User"
		greeting := greet(name)
		println(greeting)
	}
	```

	This code defines a simple Go program that prompts the user for their name and then displays a greeting message.
	  951 prompt + 83 reply tokens · 1m14.981s · 8.5 tok/s

~~~

NOTE: This code is not quiet correct and probably not worth the minute of saved run time. Discarding this model.

### granite3.2:2b

~~~harvey
/clear
/ollama use granite3.2:2b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	```go
	package main

	import (
		"fmt"
	)

	func main() {
		name := askForName()
		fmt.Println(f"Hi there, {name}!")
	}

	// askForName prompts the user for their name and returns it.
	func askForName() string {
		var name string
		fmt.Print("What's your name? ")
		fmt.Scan(&name)
		return name
	}
	```

	To write this program to a file, you would tag it like so:

	```bash
	```go cmd/hello/main.go   # space-separated language path
	```

	After tagging the code block in an interactive REPL as described above (without needing to run `/apply`), Harvey will automatically write the contents to `cmd/hello/main.go` for you.
	  1049 prompt + 188 reply tokens · 2m49.45s · 3.5 tok/s
	  ┌─ Write: cmd/hello/main.go ────────────────────────┐
	  └───────────────────────────────────────────────────────┘
	  [y]es  [n]o  [A]ll  [q]uit > y
	  ✓ wrote cmd/hello/main.go (0 bytes)

~~~

NOTE: This showed a correct program, it prompt to write out the code which was nice but while creating the directory structure failed to write the actual file. Discarding.


### granite3-moe:1b

~~~harvey
/clear
/ollama use granite3-moe:1b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	Here's a simple Go program that fulfills your requirements:

	```go
	package main

	import (
		"fmt"
		"os"

		"github.com/plmtg123456/knowledge-base"
	)

	func main() {
		if len(os.Args) < 2 {
			fmt.Println("Please provide a name to greet.")
			return
		}

		name := os.Args[1]
		fmt.Printf("Hi there, %s! What's your name?\n", name)
	}
	```

	You can run this program in your terminal and type in:

	```
	$ go run main.go
	```

	Then you'll be prompted to enter a name, and the program will greet you with "Hi there, [your name]! What's your name?".
	  1036 prompt + 193 reply tokens · 37.16s · 14.8 tok/s
~~~

NOTE: Wicked quick, sort of what I wanted. Will test further.

### granite3-moe:3b

~~~harvey
/clear
/ollama use granite3-moe:3b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	```bash:testout/hello.bash   ← colon-separated lang:path
	```go cmd/hello/main.go     ← space-separated lang path

	```go
	package main

	import (
	    "fmt"
	    "os"
	)

	func main() {
	    // Prompt for the name
	    name := os.Args[1]

	    // Display a greeting
	    fmt.Printf("Hi there, %s!\n", name)
	}
	```

	To run this program, save it as `hello.go` and then execute the following command in the terminal:

	```bash
	go run hello.go <name>
	```

	Replace `<name>` with the desired name when prompted.
	  1036 prompt + 158 reply tokens · 1m12.451s · 7.8 tok/s
	  ┌─ Write: testout/hello.bash   ← colon-separated lang:path ─┐
	  └───────────────────────────────────────────────────────┘
	  [y]es  [n]o  [A]ll  [q]uit > y
	  ✓ wrote testout/hello.bash   ← colon-separated lang:path (0 bytes)

~~~

NOTE: Interesting output choices. Wrote a zero byte file with the name "hello.bash   ← colon-separated lang:path", discarding but I think this was interesting considering it's speed. 

### cogito:3b

~~~harvey
/clear
/ollama use cogito:3b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	<available_skills>
	  <skill>
	    <name>hello-world</name>
	    <description>This display a simple hello world message via a skill.</description>
	    <location>/home/rsdoiel/Laboratory/.agents/skills/hello-world/SKILL.md</location>
	  </skill>
	  <skill>
	    <name>fountain-analysis</name>
	    <description>Read and actively monitor a Harvey Fountain screenplay file, parsing its structure and delivering incremental analysis as new content arrives. Use when the user asks to analyse, watch, tail, or summarise a .fountain session file. Prompts for the filename if not provided in the request.</description>
	    <location>/home/rsdoiel/Laboratory/.agents/skills/fountain-analysis/SKILL.md</location>
	  </skill>
	</available_skills>

	## Auto-execute model

	### File writes (always active)
	Whenever you produce a fenced code block tagged with a target path,
	Harvey writes it to disk immediately after your reply — no /apply needed.

	Tag format (two styles are supported):
	  ```bash:testout/hello.bash   ← colon-separated lang:path
	  ```go cmd/hello/main.go     ← space-separated lang path

	Always tag code blocks that are meant to be files. Do NOT say "run
	/apply" — Harvey handles it automatically and will confirm with the
	operator before writing.

	### Shell commands (agent mode only)
	When the operator has enabled agent mode (/agent on), wrap suggested
	shell commands in backtick /run hints:

	  `/run chmod +x testout/hello.bash`

	Harvey will confirm the command with the operator and then run it,
	injecting the output into context so you can see the result.

	When agent mode is off (the default), you may still suggest commands in
	this format — the operator can run them manually with /run.

	## Slash commands (for reference)

	| What needs to happen | Command |
	|---|---|
	| Create / write a file | tag your code block (auto-applied) |
	| Run a shell command | `/run <command>` hint (auto-run in agent mode) |
	| Read a file into context | /read <path> |
	| Search the workspace | /search <pattern> |
	| View git status / diff / log | /git <subcommand> |

	## Rules
	1. Never show fake command output. If you need execution, emit a
	   backtick `/run ...` hint.
	2. Never claim a file has been written. Tag the code block; Harvey
	   will write it and confirm the outcome.
	3. Always tag code blocks meant for files — one block per file.

	<available_skills>
	  <skill>
	    <name>hello-world</name>
	    <description>This display a simple hello world message via a skill.</description>
	    <location>/home/rsdoiel/Laboratory/.agents/skills/hello-world/SKILL.md</location>
	  </skill>
	  <skill>
	    <name>fountain-analysis</name>
	    <description>Read and actively monitor a Harvey Fountain screenplay file, parsing its structure and delivering incremental analysis as new content arrives. Use when the user asks to analyse, watch, tail, or summarise a .fountain session file. Prompts for the filename if not provided in the request.</description>
	    <location>/home/rsdoiel/Laboratory/.agents/skills/fountain-analysis/SKILL.md</location>
	  </skill>
	</available_skills>

	```go
	package main

	import "fmt"

	func main() {
	    fmt.Println("Hi there, what's your name?")
	    fmt.Scanln()
	    var name string
	    fmt.Print("Your name is: ")
	    fmt.Scanln(&name)
	    fmt.Printf("Hello %s!\n", name)
	}
	```

	<available_skills>
	  <skill>
	    <name>hello-world</name>
	    <description>This display a simple hello world message via a skill.</description>
	    <location>/home/rsdoiel/Laboratory/.agents/skills/hello-world/SKILL.md</location>
	  </skill>
	</available_skills>

	<backtick>
	 `/run chmod +x testout/hello.go`
	```go
	package main

	import "fmt"

	func main() {
	    fmt.Println("Hi there, what's your name?")
	    var name string
	    fmt.Print("Your name is: ")
	    fmt.Scanln(&name)
	    fmt.Printf("Hello %s!\n", name)
	}
	</backtick>
	  953 prompt + 935 reply tokens · 7m30.617s · 2.9 tok/s

~~~

NOTE: WOW! that was an interesting response, took almost 8 minutes but I wonder what it would have done on a really programming prompt. Needs further testing. Tried on Windows ARM box, it returned a Bash script. Doesn't seem ready for prime time, descarding.


### ministral-3:3b

~~~harvey
/clear
/ollama use ministral-3:3b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	```go:cmd/hello/main.go
	package main

	import (
		"bufio"
		"fmt"
		"os"
	)

	func main() {
		fmt.Println("Hi there, what's your name?")
		reader := bufio.NewReader(os.Stdin)
		name, _ := reader.ReadString('\n')
		name = name[:len(name)-1] // Remove trailing newline
		fmt.Printf("Hello, %s!\n", name)
	}
	```
	  964 prompt + 94 reply tokens · 3m3.412s · 2.6 tok/s
	  ┌─ Write: cmd/hello/main.go ────────────────────────┐
	  │  package main
	  │  
	  │  import (
	  │  	"bufio"
	  │  	"fmt"
	  │  	"os"
	  │  )
	  │  
	  │  … (7 more lines)
	  └───────────────────────────────────────────────────────┘
	  [y]es  [n]o  [A]ll  [q]uit > a
	  ✓ wrote cmd/hello/main.go (269 bytes)

~~~


NOTE: This one is a keeper. It could run on the Pi, gave a correct response and wrote the file out correctly following Go conventions.

### ministral-3:latest

~~~harvey
/clear
/ollama use ministral-3:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	Here's a simple Go program that does what you asked. I'll write it to a file for you:

	```go cmd/hello/main.go
	package main

	import (
		"bufio"
		"fmt"
		"os"
	)

	func main() {
		fmt.Println("Hi there, what's your name?")
		reader := bufio.NewReader(os.Stdin)
		name, _ := reader.ReadString('\n')
		fmt.Printf("Hello, %s!\n", name)
	}
	```

	This program:
	1. Prints the greeting message
	2. Reads user input from stdin
	3. Prints a personalized response with the entered name

	Would you like me to run it for you to see how it works? (I'd need to enable agent mode first with `/agent on` if you want me to execute it.)
	  964 prompt + 170 reply tokens · 7m26.301s · 1.4 tok/s
	  ┌─ Write: cmd/hello/main.go ────────────────────────┐
	  │  package main
	  │  
	  │  import (
	  │  	"bufio"
	  │  	"fmt"
	  │  	"os"
	  │  )
	  │  
	  │  … (6 more lines)
	  └───────────────────────────────────────────────────────┘
	  [y]es  [n]o  [A]ll  [q]uit > a
	  ✓ wrote cmd/hello/main.go (215 bytes)

~~~

NOTE: Took a while, almost eight minutes, but a quality response. Might be a good model to delegate complex things to, especially when Harvey has access to a Pi cluster.

### ministral-3:14b

~~~harvey
/clear
/ollama use ministral-3:14b
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	Here's a Go program that prompts for a name and replies with a greeting:

	```go:greet/main.go
	package main

	import (
			"bufio"
			"fmt"
			"os"
	)

	func main() {
			fmt.Println("Hi there, what's your name?")

			reader := bufio.NewReader(os.Stdin)
			name, _ := reader.ReadString('\n')

			// Trim the newline character from the input
			name = strings.TrimSpace(name)

			fmt.Printf("Nice to meet you, %s!\n", name)
	}
	```

	I'll write this to `greet/main.go`. Would you like me to proceed?
	Ollama (ministral-3:14b) · 133 reply + 936 ctx · 4m39.024s · 3.2 tok/s
	┌─ Write: greet/main.go ────────────────────────────┐
	│  package main
	│
	│  import (
	│     "bufio"
	│     "fmt"
	│     "os"
	│  )
	│
	│  … (11 more lines)
	└───────────────────────────────────────────────────────┘
	[y]es  [n]o  [A]ll  [q]uit > a
	✓ wrote greet/main.go (307 bytes)

~~~

NOTE: The processing time has passed eleven minutes, probably too large a model for the Pi. Aborted and discarded. This model ran OK on the Windows ARM box. Might run on the Pi if I had a GPU attached ...

### tinyllama:latest

~~~harvey
/clear
/ollama use tinyllama:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	```go
	package main

	import (
	    "fmt"
	)

	func main() {
	    name := askname()
	    
	    fmt.Println("Hi there, ", name, "!")
	}

	// Asks the user for a name and returns it as string
	func askname() string {
	    return fmt.Sprintf("What's your name? ")
	}
	```

	Output:
	```
	Hi there, John!
	```
	  1180 prompt + 105 reply tokens · 1m7.43s · 11.0 tok/s

~~~

NOTE: Not correct code and doesn't understand tooling. Discarding. On Windows ARM it got code that actually made sense. Still discarding.

### llama3.1:latest

~~~harvey
/clear
/ollama use llama3.1:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	```go
	package main

	import (
		"fmt"
		"os"
	)

	func main() {
		fmt.Println("Hi there, what's your name?")
		var name string
		fmt.Scanln(&name)
		fmt.Printf("Hello %s! It's nice to meet you.\n", name)
	}
	```

	Let me tag this code block for writing:

	```go cmd/hello/main.go 
	```
	  953 prompt + 80 reply tokens · 5m45.445s · 1.5 tok/s
	  ┌─ Write: cmd/hello/main.go ────────────────────────┐
	  └───────────────────────────────────────────────────────┘
	  [y]es  [n]o  [A]ll  [q]uit >   ✓ wrote cmd/hello/main.go (0 bytes)

~~~

NOTE: Code looks correct, wrote zero bytes do to keyboarding error (I hit enter before y/A).


### phi3.5:latest

~~~harvey
/clear
/ollama use phi3.5:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	Here is an example of how you might write this simple interaction in Bash script form:

	```bash
	#!/bin/bash

	echo "Hi there, what's your name?"
	read user_name

	if [ -z "$user_name" ]; then
	    echo "It seems like something went wrong. Please type your name."
	else
	    echo "Nice to meet you, $user_name!"
	fi
	```

	To use this script:
	1. Save it into a file called `hello_world.sh` for instance purposes with appropriate permissions using the command below (this assumes that Bash is already installed on your system):
	   ```bash
	   chmod +x hello_world.sh
	   ```
	2. Run it by typing `./hello_world.sh`. It will prompt you to enter a name, and then reply accordingly based on whether or not the input was provided:

	```
	Hi there, what's your name?
	Alice
	Nice to meet you, Alice!
	```
	or if something went wrong (you didn’t provide any text):

	```
	Hi there, what's your name?
	It seems like something went wrong. Please type your name.
	```
	Keep in mind that Bash is not the ideal environment for complex programming tasks or when you need more advanced features such as persistent state management; using an actual scripting language designed around these concepts might be beneficial, especially if this becomes a part of automating workflows regularly.
	  1120 prompt + 324 reply tokens · 6m11.389s · 2.4 tok/s

~~~

NOTE: Not sure why it decided to wirte something in Bash. It'm going to discard the model.

### gemma2:latest

~~~harvey
/clear
/ollama use gemma2:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
	```go cmd/hello/main.go
	package main

	import (
		"fmt"
		"bufio"
		"os"
	)

	func main() {
		fmt.Println("Hi there, what's your name?")
		reader := bufio.NewReader(os.Stdin)
		name, _ := reader.ReadString('\n')
		fmt.Printf("Hello %s!\n", name)
	}
	```
	  1009 prompt + 98 reply tokens · 7m51.028s · 1.5 tok/s
	  ┌─ Write: cmd/hello/main.go ────────────────────────┐
	  │  package main
	  │  
	  │  import (
	  │  	"fmt"
	  │  	"bufio"
	  │  	"os"
	  │  )
	  │  
	  │  … (6 more lines)
	  └───────────────────────────────────────────────────────┘
	  [y]es  [n]o  [A]ll  [q]uit > a
	  ✓ wrote cmd/hello/main.go (214 bytes)

~~~

NOTE: Took nearly eight minutes but the answer was correct. Keeping.

### phi4-mini:latest

~~~harvey
/clear
/ollama use phi4-mini:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	Certainly! Below is an example of how you can create such a simple interactive prompt in Golang using standard input/output:

	```go
	package main

	import (
		"bufio"
		"fmt"
		"os"
	)

	func getName() string {
		fmt.Println("Hi there, what's your name?")
		reader := bufio.NewReader(os.Stdin)
		name, _ := reader.ReadString('\n')
		return strings.TrimSpace(name) // Trim spaces like new lines
	}

	func main() {
		var userName string

		userName = getName()

		fmt.Printf("Nice to meet you %s!\n", userName)

	}
	```

	To run this Go program:
	1. Save the code in a file named `main.go`.
	2. Open your terminal and navigate to where you've saved `main.go`.
	3. Compile it by running: `$ go build -o hello main.go`
	4. Run what was compiled with `.go` extension using its filename, like so: `./hello`

	Remember that this program will run as long it's executing in a Go environment; otherwise (like writing to disk), you'll need the appropriate context which is managed outside of our current interactive setting here!
	  948 prompt + 238 reply tokens · 4m10.96s · 2.6 tok/s

~~~

NOTE: Code it correct with additional context provided. Nice function for the prompt. Four minutes wasn't too bad. Keeping.

### phi4-mini-reasoning:latest

~~~harvey
/clear
/ollama use phi4-mini-reasoning:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain
~~~

NOTE: Stopped at ten minutes, probably too large for the Pi. Discarding.


### smallthinker:latest

~~~harvey
/clear
/ollama use smallthinker:latest
Write a Go program that displays "Hi there, what's your name?", prompts for the name and then replies.
~~~

Results from test

~~~plain

	To answer this, I need to write a simple Go program that interacts with the user by asking for their name and then responding with a greeting. Let me think about how to approach this step-by-step.

	First, I'll start by importing the necessary packages. In this case, I only need `fmt` for input and output operations.

	```go
	// Package main defines a simple Go program that interacts with the user.
	package main

	import (
		"fmt"
	)
	```

	Now, let me think about the structure of the program. I need to prompt the user for their name and then display a greeting message. This means I'll use `fmt.Print` to print out the greeting and `fmt.Scanln` to read the user's input.

	Let me break this down into steps:
	1. Print "Hi there, what's your name?"
	2. Read the user's response
	3. Print a personalized greeting

	Here's how I can implement these steps:

	```go
	func main() {
		// Step 1: Prompt the user for their name.
		fmt.Print("Hi there, what's your name? ")

		// Step 2: Read the user's input.
		var name string
		fmt.Scanln(&name)
	```

	Now, let me think about how to format and print the greeting message. I need to make sure the program is clear and follows best practices for readability.

	Here's the final step:
	1. Print "Hi there, [name]! Nice to meet you."
	2. Ensure that the name is properly capitalized in the greeting

	```go
		// Step 3: Display a personalized greeting.
		fmt.Printf("Hi there, %s! Nice to meet you.\n", name)
	}
	```

	Wait, let me check if I need to handle any edge cases or validate the input. For simplicity, I'll assume that the user will provide a valid string for their name.

	Now, let's put it all together:

	```go
	// Package main defines a simple Go program that interacts with the user.
	package main

	import (
		"fmt"
	)

	func main() {
		// Prompt the user for their name.
		fmt.Print("Hi there, what's your name? ")

		// Read the user's input.
		var name string
		fmt.Scanln(&name)

		// Display a personalized greeting.
		fmt.Printf("Hi there, %s! Nice to meet you.\n", name)
	}
	```

	Fantastic! After all these calculations and steps, I can confidently say that this program will interact with the user by asking for their name and then responding with a greeting.

	Final Answer:
	```go
	// Package main defines a simple Go program that interacts with the user.
	package main

	import (
		"fmt"
	)

	func main() {
		// Prompt the user for their name.
		fmt.Print("Hi there, what's your name? ")

		// Read the user's input.
		var name string
		fmt.Scanln(&name)

		// Display a personalized greeting.
		fmt.Printf("Hi there, %s! Nice to meet you.\n", name)
	}
	```
	  951 prompt + 627 reply tokens · 5m55.47s · 2.6 tok/s

~~~

NOTE: Very interesting result. I could see this being used to either process more complex prompts or as a model to deterimine if another model is needed. Will need to do testing with being either the router to the target of the router.

The following three models needs to be tested. These are all big models so will likely take seven to ten minutes to test each. I expect their all too big.

- qwen2.5-coder:latest
- mistral-small:latest
- mistral:latest
