#!/usr/bin/env bash
# [DEPRECATED] Fly.io deployment has been superseded by Cloudflare (Pages + Worker + D1).
# Please use scripts/deploy_cloudflare.sh or `make deploy-cloudflare`.
set -euo pipefail

# Find flyctl binary
FLY_CMD=""
if command -v flyctl &>/dev/null; then
  FLY_CMD="flyctl"
elif command -v fly &>/dev/null; then
  FLY_CMD="fly"
elif [ -f "$HOME/.fly/bin/flyctl" ]; then
  FLY_CMD="$HOME/.fly/bin/flyctl"
else
  echo "❌ Error: flyctl is not installed."
  echo "Install it with: curl -L https://fly.io/install.sh | sh"
  exit 1
fi

echo "=========================================================="
echo "🚀 Fly.io Deployment for Catalog Sync Engine (Multi-Tenant)"
echo "=========================================================="
echo "Using CLI: $($FLY_CMD version)"

# Check auth status
echo "🔍 Checking Fly.io authentication..."
if ! $FLY_CMD auth whoami &>/dev/null; then
  echo ""
  echo "⚠️ You are not logged in to Fly.io yet."
  echo "Please run:"
  echo "  $FLY_CMD auth login"
  echo "or set the environment variable:"
  echo "  export FLY_API_TOKEN=<your-token>"
  echo ""
  read -p "Would you like to log in now? [y/N]: " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    $FLY_CMD auth login
  else
    echo "Aborting deployment until authenticated."
    exit 1
  fi
fi

APP_NAME=$(grep '^app = ' fly.toml | cut -d '"' -f2 || echo "supplier-catalog")
REGION=$(grep '^primary_region = ' fly.toml | cut -d '"' -f2 || echo "sin")

echo "📦 Target App: $APP_NAME"
echo "🌏 Region:     $REGION"

# Ensure app exists on Fly
if ! $FLY_CMD apps list | grep -qw "$APP_NAME"; then
  echo "Creating new Fly app: $APP_NAME..."
  $FLY_CMD apps create "$APP_NAME" --org personal || true
fi

# Ensure persistent volume exists
echo "💾 Checking persistent volume: catalog_data..."
if ! $FLY_CMD volumes list -a "$APP_NAME" 2>/dev/null | grep -qw "catalog_data"; then
  echo "Creating 1GB persistent volume 'catalog_data' in region '$REGION'..."
  $FLY_CMD volumes create catalog_data -a "$APP_NAME" --region "$REGION" --size 1 --yes
else
  echo "Persistent volume 'catalog_data' already exists."
fi

# Build and Deploy
echo "🚀 Deploying application (using Fly remote builder)..."
$FLY_CMD deploy --remote-only

echo ""
echo "=========================================================="
echo "✅ Deployment completed successfully!"
echo "🌐 App URL: https://$APP_NAME.fly.dev"
echo ""
echo "Test multi-tenancy by visiting:"
echo "  https://$APP_NAME.fly.dev/?shop=shop-nike-1"
echo "  https://$APP_NAME.fly.dev/?shop=shop-adidas-2"
echo "=========================================================="
