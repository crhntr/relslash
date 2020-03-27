//+build !js !wasm

package relslash

import (
	"fmt"
	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"sort"

	"gopkg.in/src-d/go-git.v4"
)

// NewBoshReleaseBumpSetData is a "porcelain" wrapper around loading the data needed to make decisions around
// a single bosh release repo bumps
//
// TODO make releaseRepo -> releaseRepos and receive variadic list of repositories
func NewBoshReleaseBumpSetData(tileRepo, releaseRepo *git.Repository) (BoshReleaseBumpSetData, error) {
	var data BoshReleaseBumpSetData

	tileRepoBranchIterator, err := tileRepo.Branches()
	if err != nil {
		return data, fmt.Errorf("could not get local repository branches for the tile repo: %w", err)
	}
	data.TileBranches, err = SupportedTileBranches(tileRepoBranchIterator)
	if err != nil {
		return data, fmt.Errorf("could not get branch repos: %s", err)
	}

	sort.Sort(ByIncreasingGeneralAvailabilityDate(data.TileBranches))

	boshReleaseRepoDir, _ := releaseRepo.Worktree() // error should not occur when using plain open

	data.BoshReleaseName, err = BoshReleaseName(boshReleaseRepoDir.Filesystem)
	if err != nil {
		return data, err
	}

	if data.BoshReleaseName == "" {
		return data, fmt.Errorf("bosh release name was not found")
	}

	data.BoshReleaseVersions, data.BoshReleaseVersionIsSemver, err = BoshReleaseVersions(boshReleaseRepoDir.Filesystem)
	if err != nil {
		return data, err
	}

	sort.Sort(ReleasesInOrder(data.BoshReleaseVersions))

	return data, nil
}

func (data BoshReleaseBumpSetData) MapTileBranchesToBoshReleaseVersions(tileRepo *git.Repository) (map[string][]Reference, error) {
	mapping := make(map[string][]Reference)

	for _, tb := range data.TileBranches {
		tileBranch := plumbing.Reference(tb)
		wt, _ := tileRepo.Worktree()

		fmt.Printf("checking out tile repository at %q\n", tileBranch.Name().Short())

		if err := wt.Checkout(&git.CheckoutOptions{Branch: tileBranch.Name(), Force: true}); err != nil {
			return mapping, err
		}

		lock, err := KilnfileLock(wt.Filesystem)
		if err != nil {
			return mapping, err
		}

		releaseLock, _, err := ReleaseLockWithName(data.BoshReleaseName, lock.Releases)
		if err != nil {
			return mapping, err
		}

		v, err := semver.NewVersion(releaseLock.Version)

		mapping[v.String()] = append(mapping[v.String()], tb)
	}
	return mapping, nil
}
