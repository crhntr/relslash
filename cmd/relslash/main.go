package main

import (
	"fmt"
	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	EnvironmentVariableProductTileRepo = "PRODUCT_TILE_REPO"
	EnvironmentVariableReleaseRepo     = "RELEASE_REPO"

	TileRepoRelBranchPrefix = "rel/"
	TileRepoMasterBranch = "master"
	KilnFileRemoteSource = "final-pcf-bosh-releases"
)

func main() {
	productRepoPath := os.Getenv(EnvironmentVariableProductTileRepo)
	releaseRepoPath := os.Getenv(EnvironmentVariableReleaseRepo)

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
	}

	if envVarErr != nil {
		log.Fatal(envVarErr)
	}

	tileRepo, openTileRepoErr := git.PlainOpen(productRepoPath)
	relRepo, openReleaseRepoErr := git.PlainOpen(releaseRepoPath)
	if err := anyErr(openTileRepoErr, openReleaseRepoErr); err != nil {
		log.Fatal("git failure", err)
	}

	tileBranches, err := getPrefixedBranches(tileRepo)
	if err != nil {
		log.Fatalf("could not get branch repos: %s", err)
	}

	boshReleaseHead, err := relRepo.Head()
	if err != nil{
		log.Fatalf("could not get head of bosh release repo: %s", err)
	}

	fmt.Printf("getting versions for bosh release %q (using HEAD %s)", releaseRepoPath, boshReleaseHead.Name())

	relRepoDir, _ := relRepo.Worktree() // error should not occur when using plain open


	configFinalFile, err := relRepoDir.Filesystem.Open("config/final.yml")
	if err != nil {
		log.Fatalf(`could not open bosh release's "config/final.yml" file: %s`, err)
	}
	buf, err := ioutil.ReadAll(configFinalFile)
	if err != nil {
		log.Fatalf(`could not read bosh release's "config/final.yml" file: %s`, err)
	}
	var configFinal struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(buf, &configFinal); err != nil {
		log.Fatalf(`could not parse yaml in bosh release's "config/final.yml" file: %s`, err)
	}
	boshReleaseName := configFinal.Name

	releaseFiles, err := relRepoDir.Filesystem.ReadDir("releases")
	if err != nil {
		log.Fatalf("could not read releases directory in bosh release repo: %s", err)
	}

	var (
		releaseVersions []*semver.Version
		boshReleaseVersionIsSemver bool
	)

	for _, relFile := range releaseFiles {
		name := relFile.Name()
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
			boshReleaseVersionIsSemver = true
		}

		v, err := semver.NewVersion(versionString)
		if err != nil {
			continue
		}

		releaseVersions = append(releaseVersions, v)
	}

	sort.Sort(sortedVersions(releaseVersions))
	sort.Sort(sortedBranches(tileBranches))

	for _, v := range releaseVersions {
		fmt.Println(v)
	}

	for _, tileBranch := range tileBranches {
		wt, _ := tileRepo.Worktree()

		if err := wt.Checkout(&git.CheckoutOptions{Branch:tileBranch.Name()}); err != nil {
			fmt.Printf("could not checkout tile repo at %s", tileBranch.Name())
			continue
		}

		kilnfileLock, err := wt.Filesystem.Open("Kilnfile.lock")
		if err != nil {
			fmt.Printf("could not open kilnfile: %s", err)
			continue
		}
		buf, err := ioutil.ReadAll(kilnfileLock)
		if err != nil {
			log.Fatalf(`could not read bosh release's "config/final.yml" file: %s`, err)
		}
		var lock KilnFileLock
		if err := yaml.Unmarshal(buf, &lock); err != nil {
			log.Fatalf(`could not parse yaml in bosh release's "Kilnfile.lock" file: %s`, err)
		}
		kilnfileLock.Close()

		var (
			index int
			releaseLock LockedRelease
		)

		for idx, release := range lock.Releases {
			if release.Name == boshReleaseName {
				index, releaseLock = idx, release
				break
			}
		}

		var updatedVersionString string


		switch boshReleaseVersionIsSemver {
		case true:
			fmt.Println("case when bosh release is a semver is not handled")
			switch tileBranch.Name().Short()  {
			case "master":

			default:

			}

		case false:
			updatedVersionString = strconv.FormatInt(releaseVersions[len(releaseVersions)-1].Major(), 10)
		}

		releaseLock.Version = updatedVersionString
		releaseLock.SHA1 = ""
		releaseLock.RemoteSource = KilnFileRemoteSource
		releaseLock.RemotePath = fmt.Sprintf("%[1]s/%[1]s-%[2]s.tgz", boshReleaseName, updatedVersionString)

		lock.Releases[index] = releaseLock

		buf, err = yaml.Marshal(lock)
		if err != nil {
			log.Fatalf(`could not render yaml for "Kilnfile.lock": %s`, err)
		}
		kilnfileLock, err = wt.Filesystem.OpenFile("Kilnfile.lock", os.O_WRONLY, 0644)
		if err != nil {
			fmt.Printf("could not open kilnfile: %s", err)
			continue
		}
		if ln, err := kilnfileLock.Write(buf); err != nil {
			fmt.Printf(`could not write to "kilnfile.lock": %s`, err)
			continue
		} else if ln != len(buf) {
			fmt.Printf(`could not open write the entire kilnfile.lock; wrote %d bytes`, ln)
			continue
		}

		if _, err := wt.Commit(fmt.Sprintf("bump %s to version %s", boshReleaseName, updatedVersionString), &git.CommitOptions{
			All: true,
		}); err != nil {

		}

	}
}

type KilnFileLock struct {
	Releases []LockedRelease `yaml:"releases"`
}

type LockedRelease struct {
	Name string `yaml:"name"`
	Version string `yaml:"name"`
	SHA1 string `yaml:"sha1"`
	RemoteSource string `yaml:"remote_source"`
	RemotePath string `yaml:"remote_path"`
}

type sortedVersions []*semver.Version

func (sv sortedVersions) Len() int {
	return len(sv)
}

func (sv sortedVersions) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv sortedVersions) Less(i, j int) bool {
	return sv[i].LessThan(sv[j])
}

type sortedBranches []plumbing.Reference

func (sv sortedBranches) Len() int {
	return len(sv)
}

func (sv sortedBranches) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv sortedBranches) Less(i, j int) bool {
	return strings.Compare(sv[i].Name().Short(), sv[j].Name().Short()) < 1
}

func getPrefixedBranches(repository *git.Repository) ([]plumbing.Reference, error) {
	refIter, err := repository.Branches()
	if err != nil {
		return nil, err
	}

	var tileReleaseBranches []plumbing.Reference

	err = refIter.ForEach(func(ref *plumbing.Reference) error {
		if name := ref.Name().Short(); name == TileRepoMasterBranch || strings.HasPrefix(name, TileRepoRelBranchPrefix) {
			tileReleaseBranches = append(tileReleaseBranches, *ref)
		}
		return nil
	})

	return tileReleaseBranches, err
}

func anyErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
