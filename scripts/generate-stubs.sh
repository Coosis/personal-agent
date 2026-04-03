#!/bin/bash
# Generate type stubs for pyright

cd "$(dirname "$0")/.."

echo "Generating type stubs..."

# Core dependencies
pyright --createstub psycopg
pyright --createstub psycopg_pool
pyright --createstub tenacity
pyright --createstub httpx

# Processing dependencies
pyright --createstub unstructured
pyright --createstub langchain
pyright --createstub langchain_text_splitters

# Config dependencies
pyright --createstub pydantic
pyright --createstub pydantic_settings

echo "Stubs generated in ./stubs/"
echo ""
echo "To regenerate (e.g., after package updates):"
echo "  rm -rf stubs/*"
echo "  ./scripts/generate-stubs.sh"
