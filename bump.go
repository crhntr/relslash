package relslash

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-billy.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/yaml.v2"
)

const (
	TileRepoRelBranchPrefix = "rel/"
	TileRepoMasterBranch    = "master"
	KilnFileRemoteSource    = "final-pcf-bosh-releases"
)

func SupportedTileBranches(iter storer.ReferenceIter) ([]Reference, error) {
	var tileReleaseBranches []Reference

	err := iter.ForEach(func(ref *plumbing.Reference) error {
		if name := ref.Name().Short(); name == TileRepoMasterBranch || strings.HasPrefix(name, TileRepoRelBranchPrefix) {
			tileReleaseBranches = append(tileReleaseBranches, Reference(*ref))
		}
		return nil
	})

	return tileReleaseBranches, err
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

type VersionsIncreasing []Version

func (sv VersionsIncreasing) Len() int {
	return len(sv)
}

func (sv VersionsIncreasing) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv VersionsIncreasing) Less(i, j int) bool {
	return (*semver.Version)(&sv[i]).LessThan((*semver.Version)(&sv[j]))
}

type VersionsDecreasing []Version

func (sv VersionsDecreasing) Len() int {
	return len(sv)
}

func (sv VersionsDecreasing) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv VersionsDecreasing) Less(i, j int) bool {
	return !(*semver.Version)(&sv[i]).LessThan((*semver.Version)(&sv[j]))
}

type ByIncreasingGeneralAvailabilityDate []Reference

func (sv ByIncreasingGeneralAvailabilityDate) Len() int {
	return len(sv)
}

func (sv ByIncreasingGeneralAvailabilityDate) Swap(i, j int) {
	sv[i], sv[j] = sv[j], sv[i]
}

func (sv ByIncreasingGeneralAvailabilityDate) Less(i, j int) bool {
	is, js := (*plumbing.Reference)(&sv[i]).Name().Short(), (*plumbing.Reference)(&sv[j]).Name().Short()
	return is != "master" && strings.Compare(is, js) < 1 // TODO: test this
}

func SetKilnfileLock(fs billy.Basic, lock KilnFileLock) error {
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

func KilnfileLock(fs billy.Basic) (KilnFileLock, error) {
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

func BoshReleaseName(fs billy.Filesystem) (string, error) {
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

func BoshReleaseVersions(fs billy.Dir) ([]Version, bool, error) {
	releaseFiles, err := fs.ReadDir("releases")
	if err != nil {
		return nil, false, err
	}

	var (
		versions []Version
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

		versions = append(versions, Version(*v))
	}

	return versions, isSemver, nil
}

func ReleaseLockWithName(name string, releases []ReleaseLock) (ReleaseLock, int, error) {
	for index, release := range releases {
		if release.Name == name {
			return release, index, nil
		}
	}

	return ReleaseLock{}, 0, fmt.Errorf("could not find release lock with name: %s", name)
}

type Reference plumbing.Reference

func (rf Reference) MarshalJSON() ([]byte, error) {
	return json.Marshal((*plumbing.Reference)(&rf).Strings())
}

func (rf *Reference) UnmarshalJSON(buf []byte) error {
	var list [2]string
	if err := json.Unmarshal(buf, &list); err != nil {
		return err
	}

	ref := plumbing.NewReferenceFromStrings(list[0], list[1])

	(*rf) = (Reference)(*ref)

	return nil
}

type Version semver.Version

func (v Version) String() string {
	return (*semver.Version)(&v).String()
}

func (v *Version) UnmarshalJSON(buf []byte) error {
	if v == nil {
		return fmt.Errorf("reciever must not be nil")
	}

	var str string
	if err := json.Unmarshal(buf, &str); err != nil {
		return err
	}
	version, err := semver.NewVersion(str)
	if err != nil {
		return err

	}

	(*v) = (Version)(*version)

	return nil
}

func (v Version) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

type BoshReleaseBumpSetData struct {
	BoshReleaseName            string
	BoshReleaseVersionIsSemver bool
	BoshReleaseVersions        []Version
	TileBranches               []Reference
}
