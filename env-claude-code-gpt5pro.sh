#!/bin/bash
# =============================================================================
# Environment Variables: Claude Code + CLIProxyAPI + OpenRouter
# =============================================================================
# Usage: source env-claude-code-gpt5pro.sh
# =============================================================================

# CLIProxyAPI endpoint
export ANTHROPIC_BASE_URL=http://127.0.0.1:8317
export ANTHROPIC_AUTH_TOKEN=sk-dummy

# Claude Code version 2.x.x
# Sử dụng OpenRouter → GPT-5.1 / GPT-5 / GPT-4o
export ANTHROPIC_DEFAULT_OPUS_MODEL=gpt-5.1          # → openai/gpt-5.1 (strongest)
export ANTHROPIC_DEFAULT_SONNET_MODEL=gpt-5          # → openai/gpt-5
export ANTHROPIC_DEFAULT_HAIKU_MODEL=gpt-4o-mini     # → openai/gpt-4o-mini (fastest)

# Claude Code version 1.x.x (backward compatibility)
export ANTHROPIC_MODEL=gpt-5.1
export ANTHROPIC_SMALL_FAST_MODEL=gpt-4o-mini

echo "Environment variables set for Claude Code + OpenRouter"
echo ""
echo "  ANTHROPIC_BASE_URL=$ANTHROPIC_BASE_URL"
echo "  ANTHROPIC_DEFAULT_OPUS_MODEL=$ANTHROPIC_DEFAULT_OPUS_MODEL  → openai/gpt-5.1"
echo "  ANTHROPIC_DEFAULT_SONNET_MODEL=$ANTHROPIC_DEFAULT_SONNET_MODEL → openai/gpt-5"
echo "  ANTHROPIC_DEFAULT_HAIKU_MODEL=$ANTHROPIC_DEFAULT_HAIKU_MODEL → openai/gpt-4o-mini"
echo ""
echo "Ready! Run: claude \"Your prompt here\""
