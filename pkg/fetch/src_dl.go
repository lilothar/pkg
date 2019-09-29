package install

import (
	"errors"
	"github.com/genshen/pkg"
	log "github.com/sirupsen/logrus"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// download source code packages.
// files: just download files specified by map files.
func filesSrc(srcDes string, packageName string, baseUrl string, files map[string]string) error {
	// check packageName dir, if not exists, then create it.
	if err := os.MkdirAll(srcDes, 0744); err != nil {
		return err
	}

	// download files:
	for k, file := range files {
		log.WithFields(log.Fields{
			"pkg":     packageName,
			"storage": filepath.Join(srcDes, file),
		}).Info("downloading dependencies.")
		res, err := http.Get(pkg.UrlJoin(baseUrl, k))
		if err != nil {
			return err // todo rollback
		}
		if res.StatusCode >= 400 {
			return errors.New("http response code is not ok (200)")
		}
		// todo create dir
		if fp, err := os.Create(filepath.Join(srcDes, file)); err != nil { //todo create dir if file includes father dirs.
			return err // todo fallback
		} else {
			if _, err = io.Copy(fp, res.Body); err != nil {
				return err // todo fallback
			}
			log.WithFields(log.Fields{
				"pkg": packageName,
			}).Info("downloaded dependencies.")
		}
	}
	return nil
}

// download archived package source code to destination directory, usually its 'vendor/src/PackageName/'.
// srcPath is the src location of this package (vendor/src/packageName).
func archiveSrc(srcPath string, packageName string, path string) error {
	if err := os.MkdirAll(srcPath, 0744); err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"pkg":     packageName,
		"storage": srcPath,
	}).Info("downloading dependency package.")

	res, err := http.Get(path)
	if err != nil {
		return err // todo fallback
	}
	if res.StatusCode >= 400 {
		return errors.New("http response code is not ok (200)")
	}

	// save file.
	zipName := filepath.Join(srcPath, packageName+".zip")
	if fp, err := os.Create(zipName); err != nil { //todo create dir if file includes father dirs.
		return err // todo fallback
	} else {
		if _, err = io.Copy(fp, res.Body); err != nil {
			return err // todo fallback
		}
	}
	log.WithFields(log.Fields{
		"pkg": packageName,
	}).Info("downloaded dependency package.")

	// unzip
	log.WithFields(log.Fields{
		"pkg":     zipName,
		"storage": srcPath,
	}).Println("extracting package.")
	err = pkg.Unzip(zipName, srcPath)
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"pkg":     zipName,
		"storage": srcPath,
	}).Println("finished extracting package.")
	return nil
}

// params:
// gitPath:  package remote path, usually its a url.
// hash: git commit hash.
// branch: git branch.
// tag:  git tag.
func gitSrc(auths []pkg.Auth, repositoryPrefix string, packageName, gitPath, hash, branch, tag string) error {
	if err := os.MkdirAll(repositoryPrefix, 0744); err != nil {
		return err
	}

	// generate auth repository url.
	repoUrl := gitPath
	if gitUrl, err := url.Parse(gitPath); err != nil {
		return err
	} else {
		for _, auth := range auths {
			if auth.Host == gitUrl.Host {
				gitUrl.User = url.UserPassword(auth.Username, auth.Token)
				repoUrl = gitUrl.String()
				break
			}
		}
	}

	// init ReferenceName using branch and tag.
	var referenceName plumbing.ReferenceName
	if branch != "" { // checkout to a specify branch.
		log.WithFields(log.Fields{
			"pkg":        packageName,
			"repository": gitPath,
			"storage":    repositoryPrefix,
			"branch":     branch,
		}).Info("cloning repository from remote to local storage.")
		referenceName = plumbing.ReferenceName("refs/heads/" + branch)
	} else if tag != "" { // checkout to specify tag.
		log.WithFields(log.Fields{
			"pkg":        packageName,
			"repository": gitPath,
			"storage":    repositoryPrefix,
			"tag":        tag,
		}).Info("cloning repository from remote to local storage.")
		referenceName = plumbing.ReferenceName("refs/tags/" + tag)
	} else {
		log.WithFields(log.Fields{
			"pkg":        packageName,
			"repository": gitPath,
			"storage":    repositoryPrefix,
		}).Info("cloning repository from remote to local storage.")
	}

	// clone repository.
	if repos, err := git.PlainClone(repositoryPrefix, false, &git.CloneOptions{
		URL:           repoUrl,
		Progress:      os.Stdout,
		ReferenceName: referenceName, // specific branch or tag.
		//RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	}); err != nil {
		return err
	} else { // clone succeed.
		if hash != "" { // if hash is not empty, then checkout to some commit.
			worktree, err := repos.Worktree()
			if err != nil {
				return err
			}
			log.WithFields(log.Fields{
				"pkg":  packageName,
				"hash": hash,
			}).Println("checkout repository to commit.")
			// do checkout
			err = worktree.Checkout(&git.CheckoutOptions{
				Hash: plumbing.NewHash(hash),
			})
			if err != nil {
				return err
			}
		}

		// remove .git directory.
		err = os.RemoveAll(filepath.Join(repositoryPrefix, ".git"))
		if err != nil {
			return err
		}
	}
	return nil
}