---
description: 'Create a technical implementation plan for a new feature'
---

# Feature Implementation Plan

## Context Loading
1. Analyze the user's request to understand the feature requirements.
2. Review existing architecture in `README.md` and `.github/copilot-instructions.md`.
3. Search for similar existing features or patterns in the codebase using `semantic-search`.

## Planning Steps
1. **Requirement Analysis:** List the core requirements and any edge cases.
2. **Component Impact:** Identify which components need changes (API, Controller, DSProxy, etc.).
3. **API Changes:** Define any changes to `api/v1alpha1` (CRD fields).
4. **Implementation Steps:** Break down the work into atomic steps (e.g., "Add field to CRD", "Update Reconciler", "Add Unit Test").
5. **Validation:** Define how to verify the feature (Unit tests, E2E tests, manual verification).

## Output
Generate a Markdown plan with the following structure:

```markdown
# Feature: [Feature Name]

## Overview
[Brief description]

## Requirements
- [ ] Req 1
- [ ] Req 2

## Technical Design
### API Changes
[Go struct changes]

### Controller Logic
[Description of reconciliation logic]

## Implementation Plan
1. [ ] Step 1
2. [ ] Step 2
3. [ ] Step 3

## Validation
- [ ] Test case 1
- [ ] Test case 2
```

## Validation Gate
🚨 **STOP**: Ask the user to review and approve the plan before proceeding to implementation.
