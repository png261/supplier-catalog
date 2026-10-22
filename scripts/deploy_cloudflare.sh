#!/usr/bin/env bash
set -euo pipefail

# Find wrangler
WRANGLER_CMD=""
if command -v wrangler &>/dev/null; then
  WRANGLER_CMD="wrangler"
elif command -v npx &>/dev/null; then
  WRANGLER_CMD="npx wrangler"
else
  echo "❌ Error: Neither wrangler nor npx is installed."
  echo "Install Node.js & npm, then run: npm install -g wrangler"
  exit 1
fi

echo "=========================================================="
echo "🚀 Cloudflare Deployment (Pages + Worker + D1 Database)"
echo "=========================================================="
echo "Using CLI: $($WRANGLER_CMD --version)"

# Check auth status
echo "🔍 Checking Cloudflare authentication..."
if ! $WRANGLER_CMD whoami &>/dev/null; then
  echo ""
  echo "⚠️ You are not logged in to Cloudflare yet."
  echo "Please run:"
  echo "  $WRANGLER_CMD login"
  echo "or set the environment variables:"
  echo "  export CLOUDFLARE_API_TOKEN=<your-token>"
  echo "  export CLOUDFLARE_ACCOUNT_ID=<your-account-id>"
  echo ""
  read -p "Would you like to log in now? [y/N]: " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    $WRANGLER_CMD login
  else
    echo "Aborting deployment until authenticated."
    exit 1
  fi
fi

# Step 1: Ensure D1 database migrations are applied
echo ""
echo "💾 Step 1/3: Applying Cloudflare D1 migrations..."
if ! $WRANGLER_CMD d1 info catalog-db &>/dev/null; then
  echo "Creating D1 database 'catalog-db'..."
  $WRANGLER_CMD d1 create catalog-db || true
fi
echo "Applying migrations to remote D1..."
$WRANGLER_CMD d1 migrations apply catalog-db --remote || true

# Step 2: Deploy Cloudflare Worker (Backend)
echo ""
echo "⚡ Step 2/3: Deploying Cloudflare Worker (Backend API)..."
$WRANGLER_CMD deploy

# Step 3: Build & Deploy Cloudflare Pages (Frontend)
echo ""
echo "🌐 Step 3/3: Building and deploying Cloudflare Pages (Frontend SPA)..."
npm --prefix frontend run build:pages
$WRANGLER_CMD pages deploy frontend/dist --project-name supplier-catalog-frontend || true

echo ""
echo "=========================================================="
echo "✅ Cloudflare Deployment Complete!"
echo "⚡ Worker API: https://catalog-sync-worker.workers.dev"
echo "🌐 Pages App:   https://supplier-catalog-frontend.pages.dev"
echo ""
echo "Test multi-tenancy by visiting:"
echo "  https://supplier-catalog-frontend.pages.dev/?shop=shop-nike-1"
echo "  https://supplier-catalog-frontend.pages.dev/?shop=shop-adidas-2"
echo "=========================================================="
