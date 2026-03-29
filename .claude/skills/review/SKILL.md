# Code Review Skill
1. Read EVERY changed file fully before reporting any issues
2. For each finding, cite the exact file and line
3. Verify error handling claims by reading the actual code path
4. Categorize as high/medium/low priority
5. After fixes, run `go build ./...` and `go test ./...` before committing
