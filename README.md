# scafctl

A configuration discovery and scaffolding tool built in Go.

## Architecture and Libraries

### CLI

- [cobra](https://github.com/spf13/cobra) powers the command-line interface, providing a proven framework for managing commands, flags, and argument parsing.
- [lipgloss](https://github.com/charmbracelet/lipgloss) delivers stylish terminal output, enabling rich colors, borders, and layouts for a polished user experience.
- [bubbletea](https://github.com/charmbracelet/bubbletea) enables interactive, event-driven terminal UIs (TUIs), supporting forms, menus, and dashboards with a model-update-view architecture.

These libraries are widely adopted in Go projects to create robust, modern CLI tools with interactive interfaces:

- **cobra**: Simplifies the creation of complex command structures (e.g., `git commit`, `git push`).
- **lipgloss**: Enhances terminal output with customizable styling.
- **bubbletea**: Facilitates dynamic, interactive workflows in the terminal.

**Integration Workflow:**

1. cobra manages the CLI structure and command parsing.
2. Upon command execution, bubbletea launches an interactive TUI.
3. lipgloss styles the TUI elements for improved readability and aesthetics.

**Example Flow:**

```text
User runs a CLI command (e.g., mytool interactive)
→ cobra parses the command and invokes the handler
→ The handler launches a bubbletea TUI
→ UI elements are styled with lipgloss
```

> **Note:**  
> For basic output, bubbletea may not be necessary. For interactive workflows, combining these libraries creates a modern, user-friendly CLI experience.

### Logging

Previously, we relied on the zap library for structured logging in CLI tools. While zap is reliable, its limited log levels—debug, info, warn, error, panic, and fatal—made it challenging to fine-tune verbosity, often resulting in excessive log output and complicating troubleshooting. By adopting logr, which offers more granular log levels, we gain precise control over logging and can efficiently filter relevant information during development and debugging.

- [zap](https://github.com/uber-go/zap) provides fast, structured logging.
- [logr](https://github.com/go-logr/logr) serves as the logging interface, supporting flexible log levels.
- [zapr](https://github.com/go-logr/zapr) acts as an adapter, implementing the logr.Logger interface using zap as the backend.