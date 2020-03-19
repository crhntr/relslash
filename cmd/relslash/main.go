package main

import (
	"fmt"
	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-billy.v4"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
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
	TileRepoMasterBranch    = "master"
	KilnFileRemoteSource    = "final-pcf-bosh-releases"
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

	boshReleaseHead, err := relRepo.Head()
	if err != nil {
		log.Fatalf("could not get head of bosh release repo: %s", err)
	}

	fmt.Printf("getting versions for bosh release %q (using HEAD %s)", releaseRepoPath, boshReleaseHead.Name())

	relRepoDir, _ := relRepo.Worktree() // error should not occur when using plain open

	boshReleaseName, err := boshReleaseName(relRepoDir.Filesystem)
	if err != nil {
		log.Fatal(err)
	}

	boshReleaseVersions, boshReleaseVersionIsSemver, err := boshReleaseVersions(relRepoDir.Filesystem)
	if err != nil {
		log.Fatal(err)
	}

	sort.Sort(releasesInOrder(boshReleaseVersions))
	sort.Sort(byIncreasingGeneralAvailabilityDate(tileBranches))

	for _, v := range boshReleaseVersions {
		fmt.Println(v)
	}

	for _, tileBranch := range tileBranches {
		wt, _ := tileRepo.Worktree()

		if err := wt.Checkout(&git.CheckoutOptions{Branch: tileBranch.Name()}); err != nil {
			fmt.Printf("could not checkout tile repo at %s\n", tileBranch.Name())
			continue
		}

		lock, err := kilnfileLock(wt.Filesystem)
		if err != nil {
			fmt.Println(err)
			continue
		}

		var (
			index       int
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
			switch tileBranch.Name().Short() {
			case "master":

			default:

			}

		case false:
			updatedVersionString = strconv.FormatInt(
				boshReleaseVersions[len(boshReleaseVersions)-1].Major(),
				10,
			)
		}

		releaseLock.Version = updatedVersionString
		releaseLock.SHA1 = ""
		releaseLock.RemoteSource = KilnFileRemoteSource
		releaseLock.RemotePath = fmt.Sprintf("%[1]s/%[1]s-%[2]s.tgz", boshReleaseName, updatedVersionString)
		lock.Releases[index] = releaseLock

		if err := setKilnfileLock(wt.Filesystem, lock); err != nil {
			log.Fatal(err)
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
	Name         string `yaml:"name"`
	Version      string `yaml:"name"`
	SHA1         string `yaml:"sha1"`
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
	file, err := fs.OpenFile("Kilnfile.lock", os.O_WRONLY, 0644)
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
		Name string `yaml:"name"`
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

func anyErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
