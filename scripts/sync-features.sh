#!/bin/bash
# Sync feature files from angzarr core repo
# Assumes angzarr core is cloned at ../angzarr or ANGZARR_CORE_PATH is set

CORE=${ANGZARR_CORE_PATH:-"../angzarr"}

if [ ! -d "$CORE/features/examples" ]; then
    echo "Error: Cannot find angzarr core at $CORE"
    echo "Clone it or set ANGZARR_CORE_PATH"
    exit 1
fi

mkdir -p features
# Copy all example features
for f in "$CORE/features/examples/"*.feature; do
    [ -f "$f" ] && cp "$f" features/
done
echo "Features synced from $CORE"
