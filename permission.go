package main

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	sdk "github.com/opensourceways/go-gitee/gitee"
	"github.com/opensourceways/repo-file-cache/models"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"
)

const ownerFile = "OWNERS"
const sigInfoFile = "sig-info.yaml"

func (bot *robot) hasPermission(
	org, repo, commenter string,
	needCheckSig bool,
	pr *sdk.PullRequestHook,
	cfg *botConfig,
	log *logrus.Entry,
) (bool, error) {
	commenter = strings.ToLower(commenter)
	p, err := bot.cli.GetUserPermissionsOfRepo(org, repo, commenter)
	if err != nil {
		return false, err
	}

	if p.Permission == "admin" || p.Permission == "write" {
		return true, nil
	}

	if needCheckSig {
		return bot.isOwnerOfSig(org, repo, commenter, pr, cfg, log)
	}

	return false, nil
}

func (bot *robot) isOwnerOfSig(
	org, repo, commenter string,
	pr *sdk.PullRequestHook,
	cfg *botConfig,
	log *logrus.Entry,
) (bool, error) {
	changes, err := bot.cli.GetPullRequestChanges(org, repo, pr.GetNumber())
	if err != nil || len(changes) == 0 {
		return false, err
	}

	pathes := sets.NewString()
	for _, file := range changes {
		if !cfg.regSigDir.MatchString(file.Filename) || strings.Count(file.Filename, "/") > 2 {
			return false, nil
		}

		pathes.Insert(filepath.Dir(file.Filename))
	}

	ownerFiles, err := bot.getFiles(org, repo, pr.GetBase().GetRef(), ownerFile, log)
	if err != nil {
		return false, err
	}

	sigInfoFiles, err := bot.getFiles(org, repo, pr.GetBase().GetRef(), sigInfoFile, log)
	if err != nil {
		return false, err
	}

	if len(ownerFiles.Files) > 0 {
		for _, v := range ownerFiles.Files {
			p := v.Path.Dir()
			if !pathes.Has(p) {
				continue
			}

			if o := decodeOwnerFile(v.Content, log); !o.Has(commenter) {
				return false, nil
			}

			pathes.Delete(p)

			if len(pathes) == 0 {
				return true, nil
			}
		}
	}

	if len(ownerFiles.Files) == 0 && len(sigInfoFiles.Files) > 0 {
		for _, v := range sigInfoFiles.Files {
			p := v.Path.Dir()
			if !pathes.Has(p) {
				continue
			}

			if o := decodeSigInfoFile(v.Content, log); !o.Has(commenter) {
				return false, nil
			}

			pathes.Delete(p)

			if len(pathes) == 0 {
				return true, nil
			}
		}
	}

	return false, nil
}

func (bot *robot) getFiles(org, repo, branch, fileName string, log *logrus.Entry) (models.FilesInfo, error) {
	files, err := bot.cacheCli.GetFiles(
		models.Branch{
			Platform: "gitee",
			Org:      org,
			Repo:     repo,
			Branch:   branch,
		},
		fileName, false,
	)
	if err != nil {
		return models.FilesInfo{}, err
	}

	if len(files.Files) == 0 {
		log.WithFields(
			logrus.Fields{
				"org":    org,
				"repo":   repo,
				"branch": branch,
			},
		).Infof("there is not %s file stored in cache.", fileName)
	}

	return files, nil
}

func decodeSigInfoFile(content string, log *logrus.Entry) sets.String {
	owners := sets.NewString()

	c, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		log.WithError(err).Error("decode file")

		return owners
	}

	var m SigInfos

	if err = yaml.Unmarshal(c, &m); err != nil {
		log.WithError(err).Error("code yaml file")

		return owners
	}

	for _, v := range m.Maintainers {
		owners.Insert(strings.ToLower(v.GiteeID))
	}

	return owners
}

func decodeOwnerFile(content string, log *logrus.Entry) sets.String {
	owners := sets.NewString()

	c, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		log.WithError(err).Error("decode file")

		return owners
	}

	var m struct {
		Maintainers []string `yaml:"maintainers"`
		Committers  []string `yaml:"committers"`
	}

	if err = yaml.Unmarshal(c, &m); err != nil {
		log.WithError(err).Error("code yaml file")

		return owners
	}

	for _, v := range m.Maintainers {
		owners.Insert(strings.ToLower(v))
	}

	for _, v := range m.Committers {
		owners.Insert(strings.ToLower(v))
	}

	return owners
}
