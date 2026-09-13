# Release 0.7.0
## Major Features and Improvements
* Add configurable provider profiles with target discovery and session-based connections.
* Add provider session authentication, command execution, output reads, and file transfers.
* Add provider-specific terminal tabs and embedded provider UI support.
* Add the example provider configuration and `RTERM_CONFIG` environment override.
* Preserve configured URL prefixes for provider APIs, WebSockets, and terminal assets, including root deployments.

# Release 0.6.0
## Major Features and Improvements
* Add embedded terminal and parent bridge support.
* Add command-level embedding permissions and trailing-slash route handling.
* Improve TTY agent synchronization for concurrent access.

# Release 0.5.0
## Major Features and Improvements
* Allow buffer size to be configurable.

# Release 0.4.0
## Major Features and Improvements
* Add support for go version < 1.22

# Release 0.3.0
## Breaking Changes
* `Command.Factory` has been removed in favor of new `Command.Args`
## Major Features and Improvements
* Add `Command.AuthCheck` to enhance security, available options: `totp`, `basic`

# Release 0.2.0
## Major Features and Improvements
* Add support to integrate with existing http server.
* Add support for multiple commands

# Release 0.1.0
Initial release of rterm.
