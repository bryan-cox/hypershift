#!/usr/bin/env bash
set -euo pipefail

# Install ai-helpers plugins on first run
MARKER="/tmp/.ai-helpers-installed"
if [ ! -f "$MARKER" ]; then
    echo "Setting up ai-helpers plugins..." >&2

    # Register the ai-helpers marketplace
    claude plugins marketplace add openshift-eng/ai-helpers 2>&1 >&2 || true

    # Install all ai-helpers plugins
    for plugin in ci jira utils git code-review; do
        echo "  Installing ${plugin}@ai-helpers..." >&2
        claude plugins install "${plugin}@ai-helpers" --scope user 2>&1 >&2 || \
            echo "  Warning: failed to install ${plugin}@ai-helpers" >&2
    done

    touch "$MARKER"
fi

# Run claude in non-interactive print mode
if [ $# -eq 0 ]; then
    exec claude --print
else
    exec claude --print "$@"
fi
