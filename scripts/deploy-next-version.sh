#!/bin/bash
set -e

# Deploy the next version by pushing to main, waiting for CI, then creating a new version tag
# Usage: deploy-next-version.sh [version]
# Example: deploy-next-version.sh v0.2.0

# Check if version is provided as argument
if [ $# -eq 1 ]; then
    NEW_TAG="$1"
    # Validate version format
    if [[ ! "$NEW_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "❌ Invalid version format. Use v<major>.<minor>.<patch> (e.g., v0.2.0)"
        exit 1
    fi
    echo "📌 Manual version specified: $NEW_TAG"
fi

# Step 1: Push to main branch
echo "🚀 Pushing to main branch..."
git push origin main

# Step 2: Monitor CI workflow with optimized polling
echo "⏳ Waiting for CI to start..."
sleep 10

# Get the latest workflow run
RUN_ID=$(gh run list --limit 1 --json databaseId --jq '.[0].databaseId')
echo "📊 Monitoring CI run $RUN_ID..."

# Poll CI status every 15 seconds (optimized for ~2m20s runtime)
MAX_WAIT=160  # seconds
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    STATUS=$(gh run view $RUN_ID --json status --jq '.status')
    CONCLUSION=$(gh run view $RUN_ID --json conclusion --jq '.conclusion // "pending"')
    
    if [ "$STATUS" = "completed" ]; then
        if [ "$CONCLUSION" = "success" ]; then
            echo "✅ CI passed successfully!"
            break
        else
            echo "❌ CI failed with conclusion: $CONCLUSION"
            exit 1
        fi
    fi
    
    echo "⏳ CI status: $STATUS (${ELAPSED}s elapsed)..."
    sleep 15
    ELAPSED=$((ELAPSED + 15))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    echo "⚠️ CI is taking longer than expected. Check manually: gh run view $RUN_ID"
    exit 1
fi

# Step 3: Determine version tag
if [ -z "$NEW_TAG" ]; then
    # No manual version provided, auto-increment
    echo "🏷️ Finding latest version tag..."
    LATEST_TAG=$(git tag -l "v*" | sort -V | tail -n1)
    if [ -z "$LATEST_TAG" ]; then
        echo "❌ No version tags found"
        exit 1
    fi

    echo "📌 Latest tag: $LATEST_TAG"

    # Extract version components and increment patch
    VERSION=${LATEST_TAG#v}
    MAJOR=$(echo $VERSION | cut -d. -f1)
    MINOR=$(echo $VERSION | cut -d. -f2)
    PATCH=$(echo $VERSION | cut -d. -f3)
    NEW_PATCH=$((PATCH + 1))
    NEW_TAG="v${MAJOR}.${MINOR}.${NEW_PATCH}"

    echo "📈 Auto-incrementing to version: $NEW_TAG"
else
    # Manual version provided, check if it already exists
    if git tag -l "$NEW_TAG" | grep -q "$NEW_TAG"; then
        echo "❌ Tag $NEW_TAG already exists!"
        exit 1
    fi
fi

# Step 4: Create and push new tag
echo "🏷️ Creating tag $NEW_TAG..."
git tag $NEW_TAG -m "Release $NEW_TAG"
git push origin $NEW_TAG

# Step 5: Monitor release workflow with optimized polling
echo "⏳ Waiting for release workflow to start..."
sleep 10

# Get the release workflow run
RELEASE_RUN_ID=$(gh run list --workflow=release.yml --limit 1 --json databaseId --jq '.[0].databaseId')
echo "📦 Monitoring release run $RELEASE_RUN_ID..."

# Poll release status every 10 seconds (optimized for ~1m05s runtime)
MAX_RELEASE_WAIT=90  # seconds
ELAPSED=0
while [ $ELAPSED -lt $MAX_RELEASE_WAIT ]; do
    STATUS=$(gh run view $RELEASE_RUN_ID --json status --jq '.status')
    CONCLUSION=$(gh run view $RELEASE_RUN_ID --json conclusion --jq '.conclusion // "pending"')
    
    if [ "$STATUS" = "completed" ]; then
        if [ "$CONCLUSION" = "success" ]; then
            echo "✅ Release completed successfully!"
            echo "🎉 Version $NEW_TAG has been deployed!"
            echo "📦 View release: https://github.com/eliezedeck/AIDevTools/releases/tag/$NEW_TAG"
            exit 0
        else
            echo "❌ Release failed with conclusion: $CONCLUSION"
            exit 1
        fi
    fi
    
    echo "⏳ Release status: $STATUS (${ELAPSED}s elapsed)..."
    sleep 10
    ELAPSED=$((ELAPSED + 10))
done

if [ $ELAPSED -ge $MAX_RELEASE_WAIT ]; then
    echo "⚠️ Release is taking longer than expected. Check manually: gh run view $RELEASE_RUN_ID"
    exit 1
fi