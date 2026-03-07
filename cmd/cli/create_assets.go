package main

import _ "embed"

//go:embed schema/task-schema.md
var taskSchema string

//go:embed schema/example-transform.yaml
var exampleTransform string

//go:embed schema/example-report.yaml
var exampleReport string
