#!/bin/bash

# Output file
OUTPUT_FILE="summary.txt"

# Clear the output file if it exists
> "$OUTPUT_FILE"

# Find all files and process them
find . -type f -not -path '*/.git/*' -not -name "summary.txt" -not -name "sum.sh" | while IFS= read -r file; do
    echo "=== $file ===" >> "$OUTPUT_FILE"
    cat "$file" >> "$OUTPUT_FILE"
    echo -e "\n" >> "$OUTPUT_FILE"
done

echo "Summary generated in $OUTPUT_FILE"
