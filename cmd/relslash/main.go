package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-billy.v4"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/yaml.v2"
)

const (
	EnvironmentVariableProductTileRepo   = "BUMP_RELEASE_PRODUCT_TILE_REPO"
	EnvironmentVariableReleaseRepo       = "BUMP_RELEASE_RELEASE_REPO"
	EnvironmentVariableCommitAuthorName  = "BUMP_RELEASE_COMMIT_AUTHOR_NAME"
	EnvironmentVariableCommitAuthorEmail = "BUMP_RELEASE_COMMIT_AUTHOR_EMAIL"

	TileRepoRelBranchPrefix = "rel/"
	TileRepoMasterBranch    = "master"
	KilnFileRemoteSource    = "final-pcf-bosh-releases"
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

	tileRepo, openTileRepoErr := git.PlainOpen(productRepoPath)
	boshReleaseRepo, openReleaseRepoErr := git.PlainOpen(releaseRepoPath)
	if err := anyErr(openTileRepoErr, openReleaseRepoErr); err != nil {
		log.Fatal("could not open git repository", err)
	}

	tileRepoBranchIterator, err := tileRepo.Branches()
	if err != nil {
		log.Fatalf("could not get local repository branches for the tile repo: %s", err)
	}
	tileBranches, err := supportedTileBranches(tileRepoBranchIterator)
	if err != nil {
		log.Fatalf("could not get branch repos: %s", err)
	}

	sort.Sort(byIncreasingGeneralAvailabilityDate(tileBranches))

	boshReleaseRepoDir, _ := boshReleaseRepo.Worktree() // error should not occur when using plain open

	boshReleaseName, err := boshReleaseName(boshReleaseRepoDir.Filesystem)
	if err != nil {
		log.Fatal(err)
	}

	if boshReleaseName == "" {
		log.Fatalf("bosh release name was not found")
	}

	boshReleaseVersions, boshReleaseVersionIsSemver, err := boshReleaseVersions(boshReleaseRepoDir.Filesystem)
	if err != nil {
		log.Fatal(err)
	}

	sort.Sort(releasesInOrder(boshReleaseVersions))

branchLoop:
	for _, tileBranch := range tileBranches {
		wt, _ := tileRepo.Worktree()

		fmt.Printf("checking out tile repository at %q\n", tileBranch.Name().Short())

		if err := wt.Checkout(&git.CheckoutOptions{Branch: tileBranch.Name(), Force: true}); err != nil {
			fmt.Printf("could not checkout tile repo: %s", err)
			continue
		}

		lock, err := kilnfileLock(wt.Filesystem)
		if err != nil {
			fmt.Println(err)
			continue
		}

		releaseLock, index, err := releaseLockWithName(boshReleaseName, lock.Releases)
		if err != nil {
			fmt.Println(err)
			continue
		}

		fmt.Printf("\tcurently the Kilnfile.lock release %q is locked to version %q\n", releaseLock.Name, releaseLock.Version)

		var updatedVersionString string

		switch boshReleaseVersionIsSemver {
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
				boshReleaseVersions[len(boshReleaseVersions)-1].Major(),
				10,
			)
		}

		if releaseLock.Version == updatedVersionString {
			fmt.Printf("\tKilnfile.lock already has the latest (%q) bosh release for release %q\n", releaseLock.Version, releaseLock.Name)
			continue
		}

		releaseLock.Version = updatedVersionString // the update

		releaseLock.SHA1 = ""
		releaseLock.RemoteSource = KilnFileRemoteSource
		releaseLock.RemotePath = fmt.Sprintf("%[1]s/%[1]s-%[2]s.tgz", boshReleaseName, updatedVersionString)
		lock.Releases[index] = releaseLock

		if err := setKilnfileLock(wt.Filesystem, lock); err != nil {
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

		commitSHA, err := wt.Commit(fmt.Sprintf("bump %s to version %s", boshReleaseName, updatedVersionString), &git.CommitOptions{
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

type KilnFileLock struct {
	Releases []ReleaseLock `yaml:"releases"`
}

type ReleaseLock struct {
	Name         string `yaml:"name"`
	SHA1         string `yaml:"sha1,omitempty"`
	Version      string `yaml:"version"`
	RemoteSource string `yaml:"remote_source"`
	RemotePath   string `yaml:"remote_path"`
}

type releasesInOrder []*semver.Version

func (sv releasesInOrder) Len() int {
	return len(sv)
}

func (sv releasesInOrder) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv releasesInOrder) Less(i, j int) bool {
	return sv[i].LessThan(sv[j])
}

type byIncreasingGeneralAvailabilityDate []plumbing.Reference

func (sv byIncreasingGeneralAvailabilityDate) Len() int {
	return len(sv)
}

func (sv byIncreasingGeneralAvailabilityDate) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv byIncreasingGeneralAvailabilityDate) Less(i, j int) bool {
	is, js := sv[i].Name().Short(), sv[j].Name().Short()

	return is != "master" && strings.Compare(is, js) < 1 // TODO: test this
}

func setKilnfileLock(fs billy.Basic, lock KilnFileLock) error {
	buf, err := yaml.Marshal(lock)
	if err != nil {
		log.Fatalf(`could not render yaml for "Kilnfile.lock": %s`, err)
	}
	file, err := fs.OpenFile("Kilnfile.lock", os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("could not open kilnfile: %s", err)
	}
	defer file.Close()

	if ln, err := file.Write(buf); err != nil {
		return err
	} else if ln != len(buf) {
		return fmt.Errorf(`could not open write the entire kilnfile.lock; wrote %d bytes`, ln)
	}

	return nil
}

func kilnfileLock(fs billy.Basic) (KilnFileLock, error) {
	var lock KilnFileLock

	kilnfileLock, err := fs.Open("Kilnfile.lock")
	if err != nil {
		return lock, err
	}
	defer kilnfileLock.Close()

	buf, err := ioutil.ReadAll(kilnfileLock)
	if err != nil {
		return lock, fmt.Errorf(`could not read bosh release's "config/final.yml" file: %s`, err)
	}

	if err := yaml.Unmarshal(buf, &lock); err != nil {
		return lock, fmt.Errorf(`could not parse yaml in bosh release's "Kilnfile.lock" file: %s`, err)
	}

	return lock, nil
}

func supportedTileBranches(iter storer.ReferenceIter) ([]plumbing.Reference, error) {
	var tileReleaseBranches []plumbing.Reference

	err := iter.ForEach(func(ref *plumbing.Reference) error {
		if name := ref.Name().Short(); name == TileRepoMasterBranch || strings.HasPrefix(name, TileRepoRelBranchPrefix) {
			tileReleaseBranches = append(tileReleaseBranches, *ref)
		}
		return nil
	})

	return tileReleaseBranches, err
}

func boshReleaseName(fs billy.Filesystem) (string, error) {
	file, err := fs.Open("config/final.yml")
	if err != nil {
		return "", fmt.Errorf(`could not open bosh release's "config/final.yml" file: %s`, err)
	}
	buf, err := ioutil.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf(`could not read bosh release's "config/final.yml" file: %s`, err)
	}
	var configFinal struct {
		Name string `yaml:"final_name"`
	}
	if err := yaml.Unmarshal(buf, &configFinal); err != nil {
		return "", fmt.Errorf(`could not parse yaml in bosh release's "config/final.yml" file: %s`, err)
	}
	return configFinal.Name, nil
}

func boshReleaseVersions(fs billy.Dir) ([]*semver.Version, bool, error) {
	releaseFiles, err := fs.ReadDir("releases")
	if err != nil {
		return nil, false, err
	}

	var (
		versions []*semver.Version
		isSemver bool
	)

	for _, file := range releaseFiles {
		name := file.Name()
		if !strings.HasSuffix(name, ".yml") {
			continue
		}
		name = strings.TrimSuffix(name, ".yml")

		segments := strings.Split(name, "-")
		if len(segments) == 0 {
			continue
		}

		versionString := segments[len(segments)-1]

		switch strings.Count(versionString, ".") {
		case 1, 2:
			isSemver = true
		}

		v, err := semver.NewVersion(versionString)
		if err != nil {
			continue
		}

		versions = append(versions, v)
	}

	return versions, isSemver, nil
}

func releaseLockWithName(name string, releases []ReleaseLock) (ReleaseLock, int, error) {
	for index, release := range releases {
		if release.Name == name {
			return release, index, nil
		}
	}

	return ReleaseLock{}, 0, fmt.Errorf("could not find release lock with name: %s", name)
}

func anyErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
