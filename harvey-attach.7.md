%harvey(7) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

ATTACH — attach a file to the conversation in the most useful form

# SYNOPSIS

/attach FILE

# DESCRIPTION

/attach reads FILE and injects it into the conversation as a user-role
message, choosing the representation that best matches what the active
route can accept:

  Image files (JPEG, PNG, GIF, WebP)
    If the active route reports vision capability, the image is encoded
    as a base64 data-URL ContentPart and sent natively — the model sees
    the actual pixels. If the route has no vision capability, a text
    description (filename, MIME type, file size) is injected instead so
    the turn still records that an attachment was attempted, and a tip is
    printed suggesting an @mention route for vision.

  PDF files
    Text is extracted using the poppler utilities (pdfinfo, pdftotext,
    pdfimages) and injected into the conversation. A 20-page cap applies
    to keep context window usage reasonable; specify /read-pdf FILE PAGES
    for a specific range. Diagram-only pages are flagged as incomplete.

  Text and source-code files (≤ 256 KB)
    The file content is injected as plain text, identical to /read.
    Files larger than 256 KB are rejected; ingest large text with
    kb document ingest.

  Binary files
    Rejected with a clear error. Use an appropriate converter first.

FILE may be an absolute path, a home-relative path (~/...), or a path
relative to the current working directory. Unlike /read, /attach is not
restricted to the workspace.

# EXAMPLES

Attach an image to the next turn on a vision-capable route:

~~~
  harvey > /route add pi2 ollama://192.168.1.12:11434 llama3.2-vision
  harvey > /attach ~/screenshots/error.png
  harvey > @pi2 What does this error message say?
~~~

Attach a PDF for the model to reason about:

~~~
  harvey > /attach ~/docs/spec.pdf
  harvey > Summarise the module system described in this document.
~~~

Attach a local source file:

~~~
  harvey > /attach src/main.go
  harvey > What does the main function do?
~~~

# SEE ALSO

  /read FILE...       — inject workspace text files (workspace-scoped)
  /read-pdf FILE PAGES — inject a specific page range from a PDF
  /kb document ingest — make a document searchable (the installed kb)
  /route              — manage named remote endpoints
  /help read-pdf

