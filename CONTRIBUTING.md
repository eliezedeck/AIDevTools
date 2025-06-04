# Contributing to Sidekick

Thank you for your interest in contributing to Sidekick! This document provides guidelines and information for contributors.

## 🚀 Quick Start

1. **Fork the repository** and clone your fork
2. **Install Go 1.23** or later
3. **Run the project locally**:
   ```bash
   cd sidekick
   go mod download
   go run main.go processes.go notifications.go
   ```

## 🛠️ Development Setup

### Prerequisites

- Go 1.23 or later
- Git
- macOS (for testing audio notifications)

### Building

```bash
cd sidekick
go build -o sidekick main.go processes.go notifications.go
```

### Running Tests

```bash
cd sidekick
go test -v ./...
```

### Code Quality

We use several tools to maintain code quality:

```bash
# Format code
go fmt ./...

# Vet code
go vet ./...

# Run linter (requires golangci-lint)
golangci-lint run
```

## 📝 Guidelines

### Code Style

- Follow standard Go conventions and idioms
- Use `gofmt` for consistent formatting
- Write clear, self-documenting code
- Add comments for exported functions and complex logic
- Keep functions focused and reasonably sized

### Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:
```
feat: add process delay functionality
fix: prevent memory leak in ring buffer
docs: update README with new examples
```

### Pull Requests

1. **Create a feature branch** from `main`
2. **Make your changes** following the guidelines above
3. **Add tests** for new functionality
4. **Update documentation** if needed
5. **Ensure CI passes** (formatting, linting, tests)
6. **Submit a pull request** with a clear description

#### PR Description Template

```markdown
## Summary
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Tests pass locally
- [ ] Added new tests for functionality
- [ ] Manual testing completed

## Checklist
- [ ] Code follows style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
- [ ] No breaking changes (or clearly documented)
```

## 🧪 Testing

### Unit Tests

Write unit tests for new functionality:

```bash
cd sidekick
go test -v ./...
```

### Integration Testing

Test with real MCP clients:

1. Build the binary
2. Configure with Claude Code
3. Test tool functionality
4. Verify cross-platform compatibility

### Manual Testing Checklist

- [ ] Process spawning works on target platforms
- [ ] Audio notifications work on macOS
- [ ] Ring buffer management handles large outputs
- [ ] Error handling works correctly
- [ ] Memory usage remains stable

## 🐛 Bug Reports

When reporting bugs, please include:

1. **Environment**: OS, Go version, Claude Code version
2. **Steps to reproduce** the issue
3. **Expected behavior** vs actual behavior
4. **Error messages** or logs
5. **Configuration** (if relevant)

## 💡 Feature Requests

For new features:

1. **Check existing issues** to avoid duplicates
2. **Describe the use case** and problem being solved
3. **Propose a solution** or approach
4. **Consider backwards compatibility**

## 🏗️ Architecture

### Project Structure

```
.
├── sidekick/           # Main Go module
│   ├── main.go        # MCP server setup and tool registration
│   ├── processes.go   # Process management functionality
│   ├── notifications.go # Audio notification functionality (macOS)
│   ├── go.mod         # Go module definition
│   └── go.sum         # Dependency checksums
├── .github/           # GitHub workflows and templates
├── README.md          # Project documentation
└── install.sh         # Installation script
```

### Key Components

- **MCP Server**: Uses `mark3labs/mcp-go` for protocol implementation
- **Process Manager**: Thread-safe process tracking with ring buffers
- **Notification System**: macOS-specific audio and TTS integration
- **Ring Buffers**: Memory-efficient output storage for long-running processes

## 📚 Resources

- [Model Context Protocol Specification](https://modelcontextprotocol.io/)
- [mark3labs/mcp-go Documentation](https://github.com/mark3labs/mcp-go)
- [Go Documentation](https://golang.org/doc/)
- [Claude Code Documentation](https://docs.anthropic.com/en/docs/claude-code)

## 🤝 Code of Conduct

We are committed to providing a welcoming and inclusive experience for everyone. Please be respectful and professional in all interactions.

## 📞 Getting Help

- **Documentation**: Check the README and this contributing guide
- **Issues**: Search existing issues or create a new one
- **Discussions**: Use GitHub Discussions for questions and ideas

Thank you for contributing to Sidekick! 🎉