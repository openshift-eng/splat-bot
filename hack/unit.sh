#!/bin/sh
echo "Running Unit Tests"
UNIT=true PROMPT_PATH=$(pwd)/pkg/knowledge/test/knowledge_prompts go test ./pkg/...

make test