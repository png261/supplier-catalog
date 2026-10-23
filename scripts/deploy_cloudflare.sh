#!/usr/bin/env bash
set -euo pipefail

CF_D1_DATABASE_NAME="${CF_D1_DATABASE_NAME:-catalog-db}"
CF_API_PROJECT_NAME="${CF_API_PROJECT_NAME:-supplier-catalog-api}"
CF_FRONTEND_PROJECT_NAME="${CF_FRONTEND_PROJECT_NAME:-supplier-catalog-frontend}"
CF_DEPLOY_BRANCH="${CF_DEPLOY_BRANCH:-main}"
export VITE_API_URL="${VITE_API_URL:-https://${CF_API_PROJECT_NAME}.pages.dev}"

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
  exit 1
fi

# Step 1: Ensure D1 database migrations are applied
echo ""
echo "💾 Step 1/3: Applying Cloudflare D1 migrations (${CF_D1_DATABASE_NAME})..."
if ! $WRANGLER_CMD d1 info "${CF_D1_DATABASE_NAME}" &>/dev/null; then
  echo "Creating D1 database '${CF_D1_DATABASE_NAME}'..."
  $WRANGLER_CMD d1 create "${CF_D1_DATABASE_NAME}" || true
fi
$WRANGLER_CMD d1 migrations apply "${CF_D1_DATABASE_NAME}" --remote || true

# Step 2: Deploy Cloudflare Backend API
echo ""
echo "⚡ Step 2/3: Deploying Cloudflare Backend API (${CF_API_PROJECT_NAME})..."
$WRANGLER_CMD deploy --dry-run --outdir worker/dist-pages
cp worker/dist-pages/index.js worker/dist-pages/_worker.js
rm -f worker/dist-pages/index.js*
(cd worker && $WRANGLER_CMD pages deploy dist-pages --project-name "${CF_API_PROJECT_NAME}" --branch "${CF_DEPLOY_BRANCH}" --commit-dirty=true) || true
rm -rf worker/dist-pages

# Step 3: Build & Deploy Cloudflare Pages (Frontend)
echo ""
echo "🌐 Step 3/3: Building and deploying Cloudflare Pages (${CF_FRONTEND_PROJECT_NAME})..."
make build-frontend
npm --prefix frontend run build:pages
if [ -f "docs/demo/demo_walkthrough.mp4" ]; then
  cp docs/demo/demo_walkthrough.mp4 web/static/demo_walkthrough.mp4
fi
$WRANGLER_CMD pages deploy web/static --project-name "${CF_FRONTEND_PROJECT_NAME}" --branch "${CF_DEPLOY_BRANCH}" --commit-dirty=true || true
rm -f web/static/demo_walkthrough.mp4

echo ""
echo "=========================================================="
echo "✅ Cloudflare Deployment Complete!"
echo "⚡ Backend API: ${VITE_API_URL}"
echo "📚 Swagger UI:  ${VITE_API_URL}/swagger/index.html"
echo "🌐 Pages App:   https://${CF_FRONTEND_PROJECT_NAME}.pages.dev"
echo "=========================================================="
