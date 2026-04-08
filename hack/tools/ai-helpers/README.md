# AI Helpers Container

A container image that runs [Claude Code](https://claude.com/claude-code) with [ai-helpers](https://github.com/openshift-eng/ai-helpers) plugins via Google Cloud Vertex AI. Designed to run in GitHub Actions, Prow jobs, or anywhere a container can run.

## Building

```bash
# From the repository root
podman build -f Dockerfile.ai-helpers -t ai-helpers .
```

## Usage

```bash
# Simple prompt
podman run --rm \
  -e ANTHROPIC_VERTEX_PROJECT_ID=<gcp-project-id> \
  -e GOOGLE_APPLICATION_CREDENTIALS=/gcp/key.json \
  -v /path/to/gcp-credentials.json:/gcp/key.json:ro \
  ai-helpers "What is HyperShift?"

# With explicit tool permissions (like the jira-agent uses)
podman run --rm \
  -e ANTHROPIC_VERTEX_PROJECT_ID=<gcp-project-id> \
  -e GOOGLE_APPLICATION_CREDENTIALS=/gcp/key.json \
  -v /path/to/gcp-credentials.json:/gcp/key.json:ro \
  ai-helpers \
  --allowedTools "Bash Read Write Edit Grep Glob WebFetch" \
  "Using jira:solve, fix OCPBUGS-12345"

# Pipe a prompt via stdin
echo "Analyze this test failure: ..." | podman run -i --rm \
  -e ANTHROPIC_VERTEX_PROJECT_ID=<gcp-project-id> \
  -e GOOGLE_APPLICATION_CREDENTIALS=/gcp/key.json \
  -v /path/to/gcp-credentials.json:/gcp/key.json:ro \
  ai-helpers
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ANTHROPIC_VERTEX_PROJECT_ID` | Yes | - | GCP project ID with Vertex AI + Claude enabled |
| `GOOGLE_APPLICATION_CREDENTIALS` | Yes | - | Path to GCP service account JSON key (mount into container) |
| `CLOUD_ML_REGION` | No | `us-east5` | GCP region for Vertex AI |
| `CLAUDE_CODE_USE_VERTEX` | No | `1` | Set by the image; enables Vertex AI auth |
| `GITHUB_TOKEN` | No | - | GitHub token for operations that interact with GitHub |

## How It Works

1. The image ships a native Claude Code binary (installed via `curl -fsSL https://claude.ai/install.sh | bash`, no npm needed)
2. On first run, the entrypoint registers the [openshift-eng/ai-helpers](https://github.com/openshift-eng/ai-helpers) marketplace and installs plugins (ci, jira, utils, git, code-review)
3. All arguments are forwarded to `claude --print`, so any Claude Code flags work (e.g. `--allowedTools`, `--system-prompt`, `--model`, `--max-turns`)
4. Claude's response is printed to stdout

## Permissions

The container does **not** use `--dangerously-skip-permissions`. Use `--allowedTools` to explicitly whitelist the tools each task needs. See the [jira-agent step registry](https://github.com/openshift/release/tree/main/ci-operator/step-registry/hypershift/jira-agent) for examples.
