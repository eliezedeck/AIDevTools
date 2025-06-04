# Security Policy

## 🔒 Supported Versions

We release patches for security vulnerabilities in the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## 🚨 Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please follow these guidelines:

### Preferred Method: Private Disclosure

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, please report security vulnerabilities by:

1. **Email**: Send details to `security@aidevtools.com` (if available)
2. **GitHub Security Advisories**: Use GitHub's private vulnerability reporting feature
3. **Direct contact**: Reach out to maintainers through private channels

### What to Include

When reporting a vulnerability, please provide:

- **Description**: Clear description of the vulnerability
- **Impact**: What could an attacker achieve?
- **Reproduction**: Step-by-step instructions to reproduce
- **Environment**: OS, Go version, Sidekick version
- **Proof of Concept**: Code or commands demonstrating the issue (if safe)

### Response Timeline

- **Acknowledgment**: Within 48 hours
- **Initial Assessment**: Within 7 days
- **Status Updates**: Weekly until resolved
- **Resolution**: Target 30 days for critical issues, 90 days for others

## 🛡️ Security Considerations

### Process Management

Sidekick spawns and manages system processes, which introduces security considerations:

#### Risks
- **Command Injection**: Malicious input could execute unintended commands
- **Process Escalation**: Spawned processes inherit permissions
- **Resource Exhaustion**: Unbounded process creation or memory usage
- **Information Disclosure**: Process output might contain sensitive data

#### Mitigations
- **Input Validation**: All command arguments are properly escaped
- **Process Isolation**: Each process runs in its own process group
- **Resource Limits**: Ring buffers prevent unbounded memory growth
- **Clean Termination**: Graceful shutdown handles process cleanup
- **No Privilege Escalation**: Processes run with same permissions as Sidekick

### MCP Server Security

#### Transport Security
- **Local Communication**: Uses stdio transport (no network exposure)
- **Authentication**: Relies on host system user authentication
- **Sandboxing**: Runs within Claude Code's security context

#### Data Handling
- **No Persistence**: No data stored permanently on disk
- **Memory Management**: Ring buffers automatically discard old data
- **Input Sanitization**: All MCP inputs validated before processing

### macOS Notifications

#### System Integration
- **Audio Commands**: Uses system `afplay` and `say` commands
- **No Network**: No external connections for notifications
- **System Permissions**: May require microphone/audio permissions

## 🔧 Security Best Practices

### For Users

1. **Keep Updated**: Always use the latest version
2. **Verify Downloads**: Check checksums for binary downloads
3. **Principle of Least Privilege**: Run with minimal necessary permissions
4. **Monitor Processes**: Review spawned processes in production environments
5. **Network Isolation**: Consider network restrictions for spawned processes

### For Developers

1. **Input Validation**: Always validate and sanitize inputs
2. **Error Handling**: Don't expose internal details in error messages
3. **Resource Management**: Implement proper resource cleanup
4. **Dependency Updates**: Keep dependencies current
5. **Security Testing**: Include security scenarios in testing

## 🚫 Out of Scope

The following are generally considered out of scope for vulnerability reports:

- **Social Engineering**: Issues requiring user cooperation beyond normal usage
- **Physical Access**: Vulnerabilities requiring physical system access
- **Denial of Service**: Resource exhaustion from legitimate heavy usage
- **Third-party Dependencies**: Issues in dependencies (report to upstream)
- **Configuration Issues**: Misconfigurations by users

## 📚 Security Resources

### Dependencies
- [Go Security Policy](https://golang.org/security)
- [MCP Security Considerations](https://modelcontextprotocol.io/docs/security)

### Tools
- `go mod audit` - Check for known vulnerabilities
- `gosec` - Go security checker (included in CI)
- `govulncheck` - Go vulnerability scanner

### Updates
- **Dependency Scanning**: Automated via Dependabot
- **Security Scanning**: Automated via GitHub Actions
- **Vulnerability Database**: Monitor Go vulnerability database

## 🏆 Recognition

We appreciate security researchers who help improve Sidekick's security. Contributors who report valid security vulnerabilities will be:

- **Acknowledged** in release notes (unless they prefer to remain anonymous)
- **Credited** in the project's security hall of fame
- **Notified** when fixes are released

## 📝 Disclosure Policy

After a security issue is resolved:

1. **Public Disclosure**: Security advisory published with details
2. **Timeline**: Full timeline of discovery, response, and resolution
3. **Credits**: Recognition for reporter (if desired)
4. **Lessons Learned**: Process improvements implemented

Thank you for helping keep Sidekick secure! 🔒