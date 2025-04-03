# Releasing

## When to release

- When Coreth needs to release a new feature or bug fix.

## Procedure

### Release candidate

ℹ️ you should always create a release candidate first, and only if everything is fine, you can create a release.

In this section, we create a release candidate `v0.15.0-rc.0`. We therefore assign these environment variables to simplify copying instructions:

```bash
export VERSION_RC=v0.15.0-rc.0
export VERSION=v0.15.0
```

1. Create your branch, usually from the tip of the `master` branch:

    ```bash
    git fetch origin master:master
    git checkout master
    git checkout -b "releases/$VERSION_RC"
    ```

1. Modify the [plugin/evm/version.go](../../plugin/evm/version.go) `Version` global string variable and set it to the desired `$VERSION`.
1. Ensure the AvalancheGo version used in [go.mod](../../go.mod) is [its last release](https://github.com/ava-labs/avalanchego/releases). If not, upgrade it with, for example:

    ```bash
    go get github.com/ava-labs/avalanchego@v1.13.0
    go mod tidy
    ```

    And fix any errors that may arise from the upgrade. If it requires significant changes, you may want to create a separate PR for the upgrade and wait for it to be merged before continuing with this procedure.
1. Update the [RELEASES.md](../../RELEASES.md) file with the new version.
1. Commit your changes and push the branch

    ```bash
    git add .
    git commit -S -m "chore: release $VERSION_RC"
    git push -u origin "releases/$VERSION_RC"
    ```

1. Create a pull request (PR) from your branch targeting master, for example using [`gh`](https://cli.github.com/):

    ```bash
    gh pr create --repo github.com/ava-labs/subnet-evm --base master --title "chore: release $VERSION_RC"
    ```

1. Once the PR checks pass, squash and merge it
1. Update your master branch, create a tag and push it:

    ```bash
    git checkout master
    git fetch origin master:master
    git tag "$VERSION_RC"
    git push -u origin "$VERSION_RC"
    ```

Once the tag is created, you need to test it on the Fuji testnet.

### Release

If a successful release candidate was created, you can now create a release.

Following the previous example in the [Release candidate section](#release-candidate), we will create a release `v0.15.0` indicated by the `$VERSION` variable.

1. Head to the last release candidate pull request, and **restore** the deleted branch at the bottom of the Github page.
1. Create a new release through the [Github web interface](https://github.com/ava-labs/subnet-evm/releases/new)
    1. In the "Choose a tag" box, enter `$VERSION` (`v0.15.0`)
    1. In the "Target", pick the previously restored last release candidate branch `releases/${VERSION_RC}`, for example `releases/v0.15.0-rc.0`.
    Do not select `master` as the target branch to prevent adding new master branch commits to the release.
    1. Pick the previous release, for example as `v0.14.0` in our case, since the default would be the last release candidate.
    1. Set the "Release title" to `$VERSION` (`v0.15.0`)
    1. Set the description (breaking changes, features, fixes, documentation); you might want to use the [RELEASES.md](../../RELEASES.md) document.
    1. Only tick the box "Set as the latest release"
    1. Click on the "Create release" button
1. Create a pull request upgrading the coreth dependency on [AvalancheGo](https://github.com/ava-labs/avalanchego).
