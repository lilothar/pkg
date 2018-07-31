package install

import (
	"os"
	"io"
	"log"
	"fmt"
	"flag"
	"errors"
	"net/http"
	"io/ioutil"
	"path/filepath"
	"encoding/json"
	"github.com/genshen/cmds"
	"github.com/genshen/pkg/utils"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

const (
	DlStatusEmpty = iota
	DlStatusSkip
	DlStatusOk
)

var getCommand = &cmds.Command{
	Name:        "install",
	Summary:     "install packages from existed file pkg.json",
	Description: "install packages(zip,cmake,makefile,.etc format) existed file pkg.json.",
	CustomFlags: false,
	HasOptions:  true,
}

func init() {
	var pkgHome string
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	getCommand.FlagSet = fs
	getCommand.FlagSet.StringVar(&pkgHome, "p", "./", "home path for file pkg.json")
	getCommand.FlagSet.Usage = getCommand.Usage // use default usage provided by cmds.Command.
	getCommand.Runner = &get{PkgHome: pkgHome}
	cmds.AllCommands = append(cmds.AllCommands, getCommand)
}

type get struct {
	PkgHome string // the path of 'pkg.json'
	DepTree DependencyTree
}

type DependencyTree struct {
	DepPkgContext
	Dependency []*DependencyTree
	Builder    []string // outer builder (lib used by others)
	DlStatus   int
}

type DepPkgContext struct {
	PackageName string
	SrcPath     string
}

func (get *get) PreRun() error {
	jsonPath := filepath.Join(get.PkgHome, utils.PkgFileName)
	// check pkg.json file existence.
	if fileInfo, err := os.Stat(jsonPath); err != nil {
		return err
	} else if fileInfo.IsDir() {
		return fmt.Errorf("%s is not a file", utils.PkgFileName)
	}

	return nil
	// check .vendor  and some related directory, if not exists, create it.
	// return utils.CheckVendorPath(pkgFilePath)
}

func (get *get) Run() error {
	// parse pkg.json and download source code.
	if pkgJsonPath, err := os.Open(filepath.Join(get.PkgHome, utils.PkgFileName)); err != nil { // open file
		return err
	} else {
		if bytes, err := ioutil.ReadAll(pkgJsonPath); err != nil { // read file contents
			return err
		} else {
			pkgs := utils.Pkg{}
			if err := json.Unmarshal(bytes, &pkgs); err != nil { // unmarshal json to struct
				return err
			}
			return get.dlSrc(get.PkgHome, &pkgs.Packages, &get.DepTree)
		}
	}
	// compile and install the source code.
	// besides, you can just use source code in your project (e.g. use cmake package in cmake project).
	get.DepTree.DlStatus = DlStatusEmpty
	if err := buildPkg(&get.DepTree, get.PkgHome); err != nil {
		log.Fatalln(err)
	}
	return nil
}

//
// download a package source to destination refer to installPath, including source code and installed files.
// usually src files are located at 'vendor/src/PackageName/', installed files are located at 'vendor/pkg/PackageName/'.
// pkgHome: pkgHome is where the file pkg.json is located.
func (get *get) dlSrc(pkgHome string, packages *utils.Packages, depTree *DependencyTree) error {
	// todo packages have dependencies.
	// todo check install.
	// download archive src package.
	for key, pkg := range packages.ArchivePackages {
		if err := get.archiveSrc(pkgHome, key, pkg.Path); err != nil {
			// todo rollback, clean src.
			return err
		} else {
			// if source code downloading succeed, then compile and install it;
			// besides, you can also use source code in your project (e.g. use cmake package in cmake project).
		}
	}
	// download files src, and add it to build tree.
	for key, pkg := range packages.FilesPackages {
		srcDes := utils.GetPackageSrcPath(pkgHome, key)
		dep := DependencyTree{
			Builder:       pkg.Package.Build[:],
			DlStatus:      DlStatusEmpty,
			DepPkgContext: DepPkgContext{SrcPath: srcDes, PackageName: key},
		}

		if _, err := os.Stat(srcDes); os.IsNotExist(err) {
			if err := get.filesSrc(srcDes, key, pkg.Path, pkg.Files); err != nil {
				// todo rollback, clean src.
				return err
			}
			dep.DlStatus = DlStatusOk
		} else if err != nil {
			return err
		} else {
			dep.DlStatus = DlStatusSkip
			log.Printf("skiped downloading %s in %s, because it already exists.\n", key, srcDes)
		}
		// add to dependency tree.
		depTree.Dependency = append(depTree.Dependency, &dep)
	}
	// download git src, and add it to build tree.
	for key, pkg := range packages.GitPackages {
		srcDes := utils.GetPackageSrcPath(pkgHome, key)
		dep := DependencyTree{
			Builder:       pkg.Package.Build[:],
			DlStatus:      DlStatusEmpty,
			DepPkgContext: DepPkgContext{SrcPath: srcDes, PackageName: key},
		}
		// check directory, if not exists, then create it.
		if _, err := os.Stat(srcDes); os.IsNotExist(err) {
			if err := get.gitSrc(srcDes, key, pkg.Path, pkg.Hash, pkg.Branch, pkg.Tag); err != nil {
				// todo rollback, clean src.
				return err
			}
			dep.DlStatus = DlStatusOk
		} else if err != nil {
			return err
		} else {
			dep.DlStatus = DlStatusSkip
			log.Printf("skiped downloading %s in %s, because it already exists.\n", key, srcDes)
		}
		// add to dependency tree.
		depTree.Dependency = append(depTree.Dependency, &dep)
		// install dependency for this package.
		if err := get.installSubDependency(srcDes, &dep); err != nil {
			return err
		}
	}
	return nil
}

// install dependency in a dependency, installPath is the path of sub-dependency.
// todo circle detect
func (get *get) installSubDependency(installPath string, depTree *DependencyTree) error {
	if pkgJsonPath, err := os.Open(filepath.Join(installPath, utils.PkgFileName)); err == nil { // pkg.json not exists.
		if bytes, err := ioutil.ReadAll(pkgJsonPath); err != nil { // read file contents
			return err
		} else {
			pkgs := utils.Pkg{}
			if err := json.Unmarshal(bytes, &pkgs); err != nil { // unmarshal json to struct
				return err
			}
			return get.dlSrc(get.PkgHome, &pkgs.Packages, depTree)
		}
	} else {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}
}

// download archived package source code to destination directory, usually its 'vendor/src/PackageName/'.
// srcPath is the src location of this package (vendor/src/packageName).
func (get *get) archiveSrc(srcPath string, packageName string, path string) error {
	if err := os.MkdirAll(srcPath, 0744); err != nil {
		return err
	}

	log.Printf("downloading %s to %s\n", packageName, srcPath)

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
	log.Printf("downloaded %s to %s\n", packageName, srcPath)

	// unzip
	log.Printf("unziping %s to %s\n", zipName, srcPath)
	err = utils.Unzip(zipName, srcPath)
	if err != nil {
		return err
	}
	log.Printf("finished unziping %s to %s\n", zipName, srcPath)
	return nil
}

// files: just download files specified by map files.
func (get *get) filesSrc(srcDes string, packageName string, baseUrl string, files map[string]string) error {
	// check packageName dir, if not exists, then create it.
	if err := os.MkdirAll(srcDes, 0744); err != nil {
		return err
	}

	// download files:
	for k, file := range files {
		log.Printf("downloading %s to %s\n", packageName, filepath.Join(srcDes, file))
		res, err := http.Get(utils.UrlJoin(baseUrl, k))
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
			log.Printf("downloaded %s to %s\n", packageName, filepath.Join(srcDes, file))
		}
	}

	return nil
}

// params:
// gitPath:  package remote path, usually its a url.
// hash: git commit hash.
// branch: git branch.
// tag:  git tag.
func (get *get) gitSrc(repositoryPrefix string, packageName, gitPath, hash, branch, tag string) error {
	if err := os.MkdirAll(repositoryPrefix, 0744); err != nil {
		return err
	}

	// init ReferenceName using branch and tag.
	var referenceName plumbing.ReferenceName
	if branch != "" { // checkout to a specify branch.
		log.Printf("cloning %s repository from %s to %s with branch: %s\n", packageName, gitPath, repositoryPrefix, branch)
		referenceName = plumbing.ReferenceName("refs/heads/" + branch)
	} else if tag != "" { // checkout to specify tag.
		log.Printf("cloning %s repository from %s to %s with tag: %s\n", packageName, gitPath, repositoryPrefix, tag)
		referenceName = plumbing.ReferenceName("refs/tags/" + tag)
	} else {
		log.Printf("cloning %s repository from %s to %s\n", packageName, gitPath, repositoryPrefix)
	}

	// clone repository.
	if repos, err := git.PlainClone(repositoryPrefix, false, &git.CloneOptions{
		URL:           gitPath,
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
			log.Printf("checkout %s repository to commit: %s\n", packageName, hash)
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
