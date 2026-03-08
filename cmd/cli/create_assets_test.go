package main

import (
	"strings"
	"testing"
)

func TestEmbeddedAssets_NonEmpty(t *testing.T) {
	if len(taskSchema) == 0 {
		t.Error("taskSchema is empty")
	}
	if len(exampleTransform) == 0 {
		t.Error("exampleTransform is empty")
	}
	if len(exampleReport) == 0 {
		t.Error("exampleReport is empty")
	}
}

func TestEmbeddedAssets_ContainExpectedContent(t *testing.T) {
	if !strings.Contains(taskSchema, "version: 1") {
		t.Error("taskSchema should contain 'version: 1'")
	}
	if !strings.Contains(exampleTransform, "mode: transform") {
		t.Error("exampleTransform should contain 'mode: transform'")
	}
	if !strings.Contains(exampleReport, "mode: report") {
		t.Error("exampleReport should contain 'mode: report'")
	}
}
