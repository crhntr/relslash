package main

import (
	"fmt"
	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"log"
	"os"
	"path"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"

	"github.com/crhntr/relslash"
)

const (
	EnvironmentVariableProductTileRepo   = "BUMP_RELEASE_PRODUCT_TILE_REPO"
	EnvironmentVariableReleaseRepo       = "BUMP_RELEASE_RELEASE_REPO"
	EnvironmentVariableCommitAuthorName  = "BUMP_RELEASE_COMMIT_AUTHOR_NAME"
	EnvironmentVariableCommitAuthorEmail = "BUMP_RELEASE_COMMIT_AUTHOR_EMAIL"
)

func main() {
	productRepoPath := os.Getenv(EnvironmentVariableProductTileRepo)
	releaseRepoPath := os.Getenv(EnvironmentVariableReleaseRepo)
	commitAuthorName := os.Getenv(EnvironmentVariableCommitAuthorName)
	commitAuthorEmail := os.Getenv(EnvironmentVariableCommitAuthorEmail)

	var envVarErr error
	switch {
	case productRepoPath == "":
		envVarErr = fmt.Errorf(EnvironmentVariableProductTileRepo + " variable not set")
	case !path.IsAbs(productRepoPath):
		envVarErr = fmt.Errorf(EnvironmentVariableProductTileRepo + " must be an absolute path")

	case releaseRepoPath == "":
		envVarErr = fmt.Errorf(EnvironmentVariableReleaseRepo + " variable not set")
	case !path.IsAbs(releaseRepoPath):
		envVarErr = fmt.Errorf(EnvironmentVariableReleaseRepo + " must be an absolute path")

	case commitAuthorName == "":
		envVarErr = fmt.Errorf(EnvironmentVariableCommitAuthorName + " variable not set")
	case commitAuthorEmail == "":
		envVarErr = fmt.Errorf(EnvironmentVariableCommitAuthorEmail + " variable not set")
	}

	if envVarErr != nil {
		log.Fatal(envVarErr)
	}

	tileRepo, err := git.PlainOpen(productRepoPath)
	if err != nil {
		log.Fatalf("could not open tile repo: %s", err)
	}
	boshReleaseRepo, err := git.PlainOpen(releaseRepoPath)
	if err != nil {
		log.Fatalf("could not open release repo: %s", err)
	}

	data, err := relslash.NewBoshReleaseBumpSetData(tileRepo, boshReleaseRepo)
	if err != nil {
		log.Fatal(err)
	}

branchLoop:
	for _, tb := range data.TileBranches {
		tileBranch := plumbing.Reference(tb)
		wt, _ := tileRepo.Worktree()

		fmt.Printf("checking out tile repository at %q\n", tileBranch.Name().Short())

		if err := wt.Checkout(&git.CheckoutOptions{Branch: tileBranch.Name(), Force: true}); err != nil {
			fmt.Printf("could not checkout tile repo: %s", err)
			continue
		}

		lock, err := relslash.KilnfileLock(wt.Filesystem)
		if err != nil {
			fmt.Println(err)
			continue
		}

		releaseLock, index, err := relslash.ReleaseLockWithName(data.BoshReleaseName, lock.Releases)
		if err != nil {
			fmt.Println(err)
			continue
		}

		fmt.Printf("\tcurently the Kilnfile.lock release %q is locked to version %q\n", releaseLock.Name, releaseLock.Version)

		var updatedVersionString string

		switch data.BoshReleaseVersionIsSemver {
		case true:
			fmt.Println("case when bosh release is a semver is not handled")
			continue branchLoop

			switch tileBranch.Name().Short() {
			case "master":
				// bump to highest major
			default:
				// bump to highest patch based on releaseLock
			}

		case false:
			updatedVersionString = strconv.FormatInt(
				(*semver.Version)(&data.BoshReleaseVersions[len(data.BoshReleaseVersions)-1]).Major(),
				10,
			)
		}

		if releaseLock.Version == updatedVersionString {
			fmt.Printf("\tKilnfile.lock already has the latest (%q) bosh release for release %q\n", releaseLock.Version, releaseLock.Name)
			continue
		}

		releaseLock.Version = updatedVersionString // the update

		releaseLock.SHA1 = ""
		releaseLock.RemoteSource = relslash.KilnFileRemoteSource
		releaseLock.RemotePath = fmt.Sprintf("%[1]s/%[1]s-%[2]s.tgz", data.BoshReleaseName, updatedVersionString)
		lock.Releases[index] = releaseLock

		if err := relslash.SetKilnfileLock(wt.Filesystem, lock); err != nil {
			log.Fatal(err)
		}

		status, err := wt.Status()
		if err != nil {
			log.Fatalf("could not show git status: %s", err)
		}

		if status.IsClean() {
			fmt.Printf("\ttile repository worktree is clean; no change to commit\n")
			continue
		}

		fmt.Printf("\tupadating the Kilnfile.lock release %q to %q\n", releaseLock.Name, releaseLock.Version)

		commitSHA, err := wt.Commit(fmt.Sprintf("bump %s to version %s", data.BoshReleaseName, updatedVersionString), &git.CommitOptions{
			All: true, // maybe instead of all we should check if "Kilnfile.lock" is the only "added" change
			Author: &object.Signature{
				Name:  commitAuthorName,
				Email: commitAuthorEmail,
				When:  time.Now(),
			},
		})
		if err != nil {
			log.Fatalf("could not create commit for tile repo on branch %q: %s", tileBranch.Name().Short(), err)
		}

		fmt.Printf("\tcreated a commit for the release release bump; the commit sha is %q\n", commitSHA.String())
	}
}
