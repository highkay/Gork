# Long Function Refactor Backlog

This backlog tracks production functions that currently exceed the 80-line
maintenance target. Refactors should preserve wire behavior and land behind
focused package tests.

| Priority | Function | Location | Current lines | Refactor direction | Verification |
| --- | --- | --- | ---: | --- | --- |
| P1 | `collectResponseStream` | `app/products/openai/responses_stream.go:14` | 129 | Split line decoding, event routing, and final response assembly. | `go test ./app/products/openai` |
| P1 | `runConsoleResponseAttempt` | `app/products/openai/console_responses.go:79` | 119 | Extract request build, upstream send, stream adaptation, and feedback handling. | `go test ./app/products/openai` |
| P1 | `EditImages` | `app/products/openai/images_edit.go:14` | 110 | Split multipart validation, upstream request build, response mapping, and image persistence. | `go test ./app/products/openai ./app/products/web` |
| P2 | `consumeChatLines` | `app/products/openai/chat_consume.go:18` | 107 | Extract chunk parsing, usage accumulation, and finish-state handling. | `go test ./app/products/openai` |
| P2 | `runConsoleCompletionAttempt` | `app/products/openai/console_chat.go:75` | 103 | Extract request build, stream conversion, and account feedback handling. | `go test ./app/products/openai` |

Update this file whenever one of the listed functions is split or a new
production function crosses the target.
