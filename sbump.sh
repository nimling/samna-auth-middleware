#!/bin/bash

SBUMP_ENV_FILES=${SBUMP_ENV_FILES:-".env:.env.example"}
SBUMP_VERSION_VAR=${SBUMP_VERSION_VAR:-APP_VERSION}
SBUMP_OPENAPI_FILES=${SBUMP_OPENAPI_FILES:-""}
SBUMP_PACKAGE_FILES=${SBUMP_PACKAGE_FILES:-""}

function show_help() {
    cat << "EOF"


 ███████╗██████╗ ██╗   ██╗███╗   ███╗██████╗
 ██╔════╝██╔══██╗██║   ██║████╗ ████║██╔══██╗
 ███████╗██████╔╝██║   ██║██╔████╔██║██████╔╝
 ╚════██║██╔══██╗██║   ██║██║╚██╔╝██║██╔═══╝
 ███████║██████╔╝╚██████╔╝██║ ╚═╝ ██║██║
 ╚══════╝╚═════╝  ╚═════╝ ╚═╝     ╚═╝╚═╝
             SBump it til it's hot!
                 Samna© 2025

EOF
    echo "Usage: sbump [OPTIONS] [major|minor|patch|commit]"
    echo ""
    echo "A simple script to bump semantic version numbers in .env files"
    echo ""
    echo "Options:"
    echo "  --help                  Show this help message and exit"
    echo "  --env-file=FILE         Path to .env file (can be repeated, default: .env)"
    echo "  --version-var=NAME      Name of version variable (default: VERSION)"
    echo "  --package-file=FILE     Path to package.json file (can be repeated, optional)"
    echo "  --openapi-file=FILE     Path to OpenAPI file (can be repeated, optional)"
    echo "  --push-version          Commit changes, create a git tag, and push to remote"
    echo ""
    echo "Commands:"
    echo "  major                   Bump major version (x.0.0)"
    echo "  minor                   Bump minor version (0.x.0)"
    echo "  patch                   Bump patch version (0.0.x)"
    echo "  commit                  Commit changes and create a git tag without bumping version"
    echo ""
    echo "Environment variables:"
    echo "  SBUMP_ENV_FILES      Colon-separated .env files (default: .env)"
    echo "  SBUMP_VERSION_VAR    Name of version variable (default: VERSION)"
    echo "  SBUMP_PACKAGE_FILES  Colon-separated package.json files (optional)"
    echo "  SBUMP_OPENAPI_FILES  Colon-separated OpenAPI files (optional)"
    echo ""
    echo "Examples:"
    echo "  sbump patch                     # Bump patch version in .env"
    echo "  sbump minor --push-version      # Bump minor version, commit, tag and push"
    echo "  sbump patch --env-file=.env --env-file=.env.example  # Update multiple .env files"
    exit 0
}

function parse_version() {
    local version=$1
    echo "$version" | sed -E 's/v([0-9]+)\.([0-9]+)\.([0-9]+)(-[a-zA-Z]+([0-9]+))?/\1 \2 \3 \4 \5/'
}

function calculate_new_version() {
    local current_version=$1
    local bump_type=$2

    read -r major minor patch suffix suffix_num <<< $(parse_version "$current_version")

    if [ -z "$suffix_num" ] && [ ! -z "$suffix" ]; then
        suffix_num=0
    fi

    case "$bump_type" in
        "major")
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        "minor")
            minor=$((minor + 1))
            patch=0
            ;;
        "patch")
            if [ -z "$suffix" ]; then
                patch=$((patch + 1))
            else
                suffix_num=$((suffix_num + 1))
            fi
            ;;
        *)
            echo "Invalid bump type. Use 'major', 'minor', or 'patch'"
            show_help
            ;;
    esac

    if [ -z "$suffix" ]; then
        echo "v$major.$minor.$patch"
    else
        local suffix_prefix=$(echo $suffix | sed -E 's/(-[a-zA-Z]+)([0-9]+)?/\1/')
        echo "v$major.$minor.$patch$suffix_prefix$suffix_num"
    fi
}

function find_files() {
    local pattern=$1

    if [ -z "$pattern" ]; then
        return
    fi

    if [ -d "$pattern" ]; then
        find "$pattern" -type f \( -name "*.json" -o -name "*.yaml" -o -name "*.yml" \) 2>/dev/null
    elif [ -f "$pattern" ]; then
        echo "$pattern"
    else
        find . -type f -path "$pattern" 2>/dev/null
    fi
}

function apply_version_updates() {
    local version=$1

    IFS=':' read -ra ENV_FILES_ARRAY <<< "$SBUMP_ENV_FILES"
    for env_file in "${ENV_FILES_ARRAY[@]}"; do
        if [ ! -z "$env_file" ] && [ -f "$env_file" ]; then
            update_env_file "$version" "$env_file"
        fi
    done

    if [ ! -z "$SBUMP_PACKAGE_FILES" ]; then
        IFS=':' read -ra PACKAGE_PATTERNS <<< "$SBUMP_PACKAGE_FILES"
        for pattern in "${PACKAGE_PATTERNS[@]}"; do
            while IFS= read -r file; do
                if [ -f "$file" ] && grep -q '"version":' "$file" 2>/dev/null; then
                    update_package_file "$version" "$file"
                fi
            done < <(find_files "$pattern")
        done
    fi

    if [ ! -z "$SBUMP_OPENAPI_FILES" ]; then
        IFS=':' read -ra OPENAPI_PATTERNS <<< "$SBUMP_OPENAPI_FILES"
        for pattern in "${OPENAPI_PATTERNS[@]}"; do
            while IFS= read -r file; do
                if [ -f "$file" ]; then
                    update_openapi_file "$version" "$file"
                fi
            done < <(find_files "$pattern")
        done
    fi
}

function get_version_from_sources() {
    local version=""

    if [ ! -z "$SBUMP_PACKAGE_FILES" ]; then
        IFS=':' read -ra PACKAGE_PATTERNS <<< "$SBUMP_PACKAGE_FILES"
        for pattern in "${PACKAGE_PATTERNS[@]}"; do
            while IFS= read -r file; do
                if [ -f "$file" ]; then
                    version=$(grep '"version":' "$file" | head -1 | sed -E 's/.*"version":[[:space:]]*"([^"]+)".*/\1/')
                    if [ ! -z "$version" ]; then
                        if [[ ! "$version" =~ ^v ]]; then
                            version="v$version"
                        fi
                        echo "$version"
                        return
                    fi
                fi
            done < <(find_files "$pattern")
        done
    fi

    version=$(git describe --tags --abbrev=0 2>/dev/null)
    if [ ! -z "$version" ]; then
        echo "$version"
        return
    fi

    echo "v1.0.0"
}

function initialize_env_file() {
    local version=$(get_version_from_sources)
    IFS=':' read -ra ENV_FILES_ARRAY <<< "$SBUMP_ENV_FILES"
    for env_file in "${ENV_FILES_ARRAY[@]}"; do
        if [ ! -z "$env_file" ]; then
            echo "$SBUMP_VERSION_VAR=$version" > "$env_file"
            echo "Created $env_file with version $version"
        fi
    done
}

function update_env_file() {
    local new_version=$1
    local env_file=$2
    sed -i.bak "s/^$SBUMP_VERSION_VAR=.*/$SBUMP_VERSION_VAR=$new_version/" "$env_file" && rm -f "$env_file.bak"
    echo "    ✓ Updated $env_file"
}

function update_package_file() {
    local new_version=$1
    local file=$2

    if [ -z "$file" ]; then
        return
    fi

    local version_without_v=${new_version#v}
    sed -i.bak "s/\"version\":[[:space:]]*\"[^\"]*\"/\"version\": \"$version_without_v\"/" "$file" && rm -f "$file.bak"
    echo "    ✓ Updated $file"
}

function update_openapi_file() {
    local new_version=$1
    local file=$2

    if [ -z "$file" ]; then
        return
    fi

    if [[ "$file" == *.json ]]; then
        sed -i.bak "s/\"version\":[[:space:]]*\"[^\"]*\"/\"version\": \"$new_version\"/" "$file" && rm -f "$file.bak"
        echo "    ✓ Updated $file"
    elif [[ "$file" == *.yml ]] || [[ "$file" == *.yaml ]]; then
        if command -v yq &> /dev/null; then
            yq -i '.info.version = "'"$new_version"'"' "$file"
            echo "    ✓ Updated $file"
        else
            echo "yq not found, falling back to sed"
            local line_num=$(grep -n "^\s*version:" "$file" | cut -d: -f1)
            if [ -z "$line_num" ]; then
                line_num=$(grep -n -A2 "^info:" "$file" | grep "version:" | cut -d: -f1)
            fi

            if [ -n "$line_num" ]; then
                sed -i.bak "${line_num}s/.*version:.*/  version: $new_version/" "$file" && rm -f "$file.bak"
                echo "    ✓ Updated $file"
            else
                echo "Version field not found in OpenAPI file"
                show_help
            fi
        fi
    else
        echo "Unsupported OpenAPI file format. Use .json, .yml, or .yaml"
        show_help
    fi
}

function check_tag_exists_remote() {
    local tag=$1
    if git ls-remote --tags origin 2>/dev/null | grep -q "refs/tags/$tag$"; then
        return 0
    else
        return 1
    fi
}

function cleanup_failed_version() {
    local version=$1
    echo "  🧹 Cleaning up failed version $version"

    git tag -d "$version" >/dev/null 2>&1

    git reset --soft HEAD~1 >/dev/null 2>&1

    echo "  ✓ Cleaned up local state"
}

function commit_and_push() {
    local version=$1
    local push=$2
    local error_msg=""

    if [ -z "$version" ]; then
        echo "  ❌ No version provided to commit"
        exit 1
    fi

    IFS=':' read -ra ENV_FILES_ARRAY <<< "$SBUMP_ENV_FILES"
    for env_file in "${ENV_FILES_ARRAY[@]}"; do
        if [ ! -z "$env_file" ] && [ -f "$env_file" ]; then
            if ! git diff --quiet "$env_file" 2>/dev/null; then
                git add "$env_file" 2>/dev/null
            fi
        fi
    done

    if [ ! -z "$SBUMP_PACKAGE_FILES" ]; then
        IFS=':' read -ra PACKAGE_PATTERNS <<< "$SBUMP_PACKAGE_FILES"
        for pattern in "${PACKAGE_PATTERNS[@]}"; do
            while IFS= read -r file; do
                if [ -f "$file" ] && git diff --quiet "$file" 2>/dev/null; then
                    continue
                elif [ -f "$file" ]; then
                    git add "$file" 2>/dev/null
                fi
            done < <(find_files "$pattern")
        done
    fi

    if [ ! -z "$SBUMP_OPENAPI_FILES" ]; then
        IFS=':' read -ra OPENAPI_PATTERNS <<< "$SBUMP_OPENAPI_FILES"
        for pattern in "${OPENAPI_PATTERNS[@]}"; do
            while IFS= read -r file; do
                if [ -f "$file" ] && git diff --quiet "$file" 2>/dev/null; then
                    continue
                elif [ -f "$file" ]; then
                    git add "$file" 2>/dev/null
                fi
            done < <(find_files "$pattern")
        done
    fi

    if [ -z "$(git diff --cached --name-only)" ]; then
        error_msg=$(git tag "$version" 2>&1)
        if [ $? -ne 0 ]; then
            if echo "$error_msg" | grep -q "already exists"; then
                echo "  ⚠️  Tag $version already exists locally"
                cleanup_failed_version "$version"
                return 1
            else
                echo "  ❌ Failed to create tag"
                echo "     Error: $error_msg"
                exit 1
            fi
        fi

        echo "  ✓ Created tag"
        if [ "$push" = "true" ]; then
            echo "  → Pushing tag to remote..."
            error_msg=$(git push origin "$version" 2>&1)
            if [ $? -eq 0 ]; then
                echo "  ✓ Pushed tag"
                return 0
            else
                echo "  ❌ Failed to push tag"
                echo "     Error: $error_msg"
                git tag -d "$version" >/dev/null 2>&1
                exit 1
            fi
        fi
        return 0
    fi

    error_msg=$(git commit -m "Bump version to $version" 2>&1)
    if [ $? -ne 0 ]; then
        echo "  ❌ Failed to commit"
        if echo "$error_msg" | grep -q "nothing to commit\|no changes added"; then
            echo "     No changes to commit. This might indicate a state issue."
            echo "     Try running 'git status' to check your repository state."
        elif echo "$error_msg" | grep -q "Changes not staged"; then
            echo "     There are unstaged changes in your working directory."
            echo "     This should not happen - please check your git status."
        else
            echo "     Error: $error_msg"
        fi
        exit 1
    fi

    error_msg=$(git tag "$version" 2>&1)
    if [ $? -eq 0 ]; then
        echo "  ✓ Created commit and tag"
        if [ "$push" = "true" ]; then
            echo "  → Pushing commit to remote..."
            error_msg=$(git push 2>&1)
            push_result=$?

            if [ $push_result -eq 0 ]; then
                echo "  ✓ Pushed commit"
                echo "  → Pushing tag to remote..."
                error_msg_tags=$(git push origin "$version" 2>&1)
                push_tags_result=$?

                if [ $push_tags_result -eq 0 ]; then
                    echo "  ✓ Pushed tag"
                    return 0
                else
                    if echo "$error_msg_tags" | grep -q "already exists"; then
                        echo "  ⚠️  Tag $version already exists on remote"
                        cleanup_failed_version "$version"
                        return 1
                    elif check_tag_exists_remote "$version"; then
                        echo "  ⚠️  Tag $version already exists on remote"
                        cleanup_failed_version "$version"
                        return 1
                    else
                        echo "  ❌ Failed to push tag"
                        echo "     Tag error: $error_msg_tags"
                        cleanup_failed_version "$version"
                        exit 1
                    fi
                fi
            else
                echo "  ❌ Failed to push commit"
                echo "     Push error: $error_msg"
                cleanup_failed_version "$version"
                exit 1
            fi
        else
            return 0
        fi
    else
        if echo "$error_msg" | grep -q "already exists"; then
            echo "  ⚠️  Tag $version already exists locally"
            cleanup_failed_version "$version"
            return 1
        else
            echo "  ❌ Failed to create tag"
            echo "     Error: $error_msg"
            git reset --soft HEAD~1 >/dev/null 2>&1
            exit 1
        fi
    fi
}

# Parse command line arguments
BUMP_TYPE=""
PUSH_VERSION="false"
DO_COMMIT="false"
CLI_ENV_FILES=()
CLI_PACKAGE_FILES=()
CLI_OPENAPI_FILES=()

for arg in "$@"; do
    case "$arg" in
        "--help")
            show_help
            ;;
        "--push-version")
            PUSH_VERSION="true"
            ;;
        "commit")
            DO_COMMIT="true"
            ;;
        --env-file=*)
            CLI_ENV_FILES+=("${arg#*=}")
            ;;
        --version-var=*)
            SBUMP_VERSION_VAR="${arg#*=}"
            ;;
        --package-file=*)
            CLI_PACKAGE_FILES+=("${arg#*=}")
            ;;
        --openapi-file=*)
            CLI_OPENAPI_FILES+=("${arg#*=}")
            ;;
        "major"|"minor"|"patch")
            BUMP_TYPE="$arg"
            ;;
        *)
            ;;
    esac
done

if [ ${#CLI_ENV_FILES[@]} -gt 0 ]; then
    SBUMP_ENV_FILES=$(IFS=:; echo "${CLI_ENV_FILES[*]}")
fi

if [ ${#CLI_PACKAGE_FILES[@]} -gt 0 ]; then
    SBUMP_PACKAGE_FILES=$(IFS=:; echo "${CLI_PACKAGE_FILES[*]}")
fi

if [ ${#CLI_OPENAPI_FILES[@]} -gt 0 ]; then
    SBUMP_OPENAPI_FILES=$(IFS=:; echo "${CLI_OPENAPI_FILES[*]}")
fi

# Check for uncommitted changes
if [ -n "$(git status --porcelain)" ]; then
    echo ""
    echo "┌─────────────────────────────────────────────┐"
    echo "│  ⚠️  Warning: Uncommitted Changes            │"
    echo "└─────────────────────────────────────────────┘"
    echo ""
    echo "  You have uncommitted changes in your working directory."
    echo "  Please commit or stash your changes before bumping the version."
    echo ""
    echo "  Files with changes:"
    git status --short | sed 's/^/    /'
    echo ""
    exit 1
fi

# Validate input and initialize if needed
IFS=':' read -ra ENV_FILES_ARRAY <<< "$SBUMP_ENV_FILES"
PRIMARY_ENV_FILE="${ENV_FILES_ARRAY[0]}"

for env_file in "${ENV_FILES_ARRAY[@]}"; do
    if [ ! -z "$env_file" ] && [ ! -f "$env_file" ]; then
        initialize_env_file
        break
    fi
done

# Source primary environment file
if [ -f "$PRIMARY_ENV_FILE" ]; then
    source "$PRIMARY_ENV_FILE"
fi

# Main execution logic
if [ "$DO_COMMIT" = "true" ]; then
    CURRENT_VERSION=$(grep "^$SBUMP_VERSION_VAR=" "$PRIMARY_ENV_FILE" | cut -d= -f2)
    if [ -z "$CURRENT_VERSION" ]; then
        echo "Version variable $SBUMP_VERSION_VAR not found in $PRIMARY_ENV_FILE"
        show_help
    fi
    commit_and_push "$CURRENT_VERSION" "$PUSH_VERSION"
    exit 0
fi

if [ -z "$BUMP_TYPE" ]; then
    echo "No bump type specified"
    show_help
fi

CURRENT_VERSION=$(grep "^$SBUMP_VERSION_VAR=" "$PRIMARY_ENV_FILE" | cut -d= -f2)

if [ -z "$CURRENT_VERSION" ]; then
    echo "Version variable $SBUMP_VERSION_VAR not found in $PRIMARY_ENV_FILE"
    show_help
fi

echo ""
echo "┌─────────────────────────────────────────────┐"
echo "│  📦 Version Bump Process                    │"
echo "└─────────────────────────────────────────────┘"
echo ""
echo "  Current version: $CURRENT_VERSION"
echo "  Bump type:       $BUMP_TYPE"
echo ""

MAX_RETRIES=10
RETRY_COUNT=0
SUCCESS=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    NEW_VERSION=$(calculate_new_version "$CURRENT_VERSION" "$BUMP_TYPE")

    if [ $RETRY_COUNT -gt 0 ]; then
        echo "  ⚠️  Retrying... attempt $((RETRY_COUNT + 1))/$MAX_RETRIES"
    fi

    echo "  → Target version: $NEW_VERSION"
    echo ""

    apply_version_updates "$NEW_VERSION"

    echo ""

    if [ "$PUSH_VERSION" = "true" ]; then
        if commit_and_push "$NEW_VERSION" "true"; then
            echo ""
            echo "┌─────────────────────────────────────────────┐"
            echo "│  ✨ Success!                                 │"
            echo "└─────────────────────────────────────────────┘"
            echo ""
            echo "  Version $NEW_VERSION has been published"
            echo ""
            SUCCESS=true
            break
        else
            CURRENT_VERSION="$NEW_VERSION"
            RETRY_COUNT=$((RETRY_COUNT + 1))
        fi
    else
        echo ""
        echo "  New version: $NEW_VERSION"
        echo ""
        SUCCESS=true
        break
    fi
done

if [ "$SUCCESS" = false ]; then
    echo ""
    echo "┌─────────────────────────────────────────────┐"
    echo "│  ❌ Failed                                   │"
    echo "└─────────────────────────────────────────────┘"
    echo ""
    echo "  Could not publish version after $MAX_RETRIES attempts"
    echo ""
    exit 1
fi
