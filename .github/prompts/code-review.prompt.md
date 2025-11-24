---
description: 'Review code changes against project standards'
---

# Code Review Workflow

## Context Loading
1. Review the changes in the current PR or active file.
2. Check `.github/agents/go-agent.md` for coding standards.
3. Check `api/v1alpha1/defaults.go` for default values if relevant.

## Review Checklist
- **Correctness:** Does the code do what it's supposed to do?
- **Error Handling:** Are errors checked and wrapped?
- **Naming:** Do names follow `go-agent` standards?
- **Tests:** Are there unit tests for the new logic?
- **Security:** Are there any security risks (secrets, input validation)?
- **Kubernetes:** Are K8s patterns followed (Status updates, Retries)?

## Output
Provide a structured review:

### Summary
[Brief summary of changes]

### Issues
- [Critical] Description of critical issue
- [Major] Description of major issue
- [Minor] Description of minor issue

### Suggestions
- Suggestion 1
- Suggestion 2

### Validation
- [ ] Tests pass?
- [ ] Linter passes?
