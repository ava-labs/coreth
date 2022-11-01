set -o errexit
set -o nounset
set -o pipefail

# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Tests because Docker Credentials were not present."
    exit 1
fi

# Testing specific variables
camino_testing_repo="c4tplatform/camino-testing"
caminogo_repo="c4tplatform/caminogo"
# Define default camino testing version to use
camino_testing_image="${camino_testing_repo}:master"

# Camino root directory
CAMINOETHVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the versions
source "$CAMINOETHVM_PATH"/scripts/versions.sh

# Load the constants
source "$CAMINOETHVM_PATH"/scripts/constants.sh

# Login to docker
echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

# Checks available docker tags exist
function docker_tag_exists() {
    TOKEN=$(curl -s -H "Content-Type: application/json" -X POST -d '{"username": "'${DOCKER_USERNAME}'", "password": "'${DOCKER_PASS}'"}' https://hub.docker.com/v2/users/login/ | jq -r .token)
    curl --silent -H "Authorization: JWT ${TOKEN}" -f --head -lL https://hub.docker.com/v2/repositories/$1/tags/$2/ > /dev/null
}

# Defines the camino-testing tag to use
# Either uses the same tag as the current branch or uses the default
if docker_tag_exists $camino_testing_repo $current_branch; then
    echo "$camino_testing_repo:$current_branch exists; using this image to run e2e tests"
    camino_testing_image="$camino_testing_repo:$current_branch"
else
    echo "$camino_testing_repo $current_branch does NOT exist; using the default image to run e2e tests"
fi

echo "Using $camino_testing_image for e2e tests"

# Defines the caminogo tag to use
# Either uses the same tag as the current branch or uses the default
# Disable matchup in favor of explicit tag
# TODO re-enable matchup when our workflow better supports it.
# if docker_tag_exists $caminogo_repo $current_branch; then
#     echo "$caminogo_repo:$current_branch exists; using this caminogo image to run e2e tests"
#     CAMINOGO_VERSION=$current_branch
# else
#     echo "$caminogo_repo $current_branch does NOT exist; using the default image to run e2e tests"
# fi

# pulling the camino-testing image
docker pull $camino_testing_image

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current caminogo
source "$CAMINOETHVM_PATH"/scripts/build_image.sh

# Target built version to use in camino-testing
camino_image="c4tplatform/caminogo:$build_image_id"

echo "Running Camino Image: ${camino_image}"
echo "Running Camino Testing Image: ${camino_testing_image}"
echo "Git Commit ID : ${git_commit_id}"


# >>>>>>>> camino-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"caminogoImage\":\"${camino_image}\",
    \"testBatch\":\"caminogo\"
}"
# >>>>>>>> camino-testing custom parameters <<<<<<<<<<<<<

bash "$CAMINOETHVM_PATH/.kurtosis/kurtosis.sh" \
    --tests "C-Chain Bombard WorkFlow,Dynamic Fees,Snowman++ Correct Proposers and Timestamps" \
    --custom-params "${custom_params_json}" \
    "${camino_testing_image}" \
    $@
